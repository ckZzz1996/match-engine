package funding

import (
	"context"
	"fmt"
	"sync"
	"time"

	"match-engine/pkg/position"
	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// Service 资金费率服务
type Service struct {
	positionMgr *position.Manager
	logger      *zap.Logger

	// 当前资金费率 map[symbol]FundingRate
	fundingRates map[string]*types.FundingRate
	mu           sync.RWMutex

	// 资金费率历史
	history map[string][]*types.FundingRate

	// 保险基金
	insuranceFunds map[string]*types.InsuranceFund

	// 事件通道
	paymentChan chan *types.FundingPayment

	// 配置
	defaultInterval time.Duration // 默认结算间隔 (8小时)
}

// NewService 创建资金费率服务
func NewService(positionMgr *position.Manager, logger *zap.Logger) *Service {
	return &Service{
		positionMgr:     positionMgr,
		logger:          logger,
		fundingRates:    make(map[string]*types.FundingRate),
		history:         make(map[string][]*types.FundingRate),
		insuranceFunds:  make(map[string]*types.InsuranceFund),
		paymentChan:     make(chan *types.FundingPayment, 10000),
		defaultInterval: 8 * time.Hour,
	}
}

// Start 启动资金费率服务
func (s *Service) Start(ctx context.Context) {
	go s.settlementLoop(ctx)
	go s.calculateLoop(ctx)
}

// settlementLoop 结算循环
func (s *Service) settlementLoop(ctx context.Context) {
	// 计算下一个结算时间点 (每8小时: 00:00, 08:00, 16:00 UTC)
	ticker := time.NewTicker(1 * time.Minute) // 每分钟检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			// 检查是否到达结算时间
			hour := now.UTC().Hour()
			minute := now.UTC().Minute()

			// 在 00:00, 08:00, 16:00 的第一分钟进行结算
			if (hour == 0 || hour == 8 || hour == 16) && minute == 0 {
				s.settleAllSymbols()
			}
		}
	}
}

// calculateLoop 计算资金费率循环
func (s *Service) calculateLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.calculateAllFundingRates()
		}
	}
}

// calculateAllFundingRates 计算所有合约的资金费率
func (s *Service) calculateAllFundingRates() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for symbol, rate := range s.fundingRates {
		// 资金费率 = clamp(溢价指数 + clamp(利率 - 溢价指数, -0.05%, 0.05%), -0.75%, 0.75%)
		// 简化计算: 资金费率 = (标记价 - 指数价) / 指数价 * 时间因子
		if rate.IndexPrice.IsZero() {
			continue
		}

		// 溢价 = (标记价 - 指数价) / 指数价
		premium := rate.MarkPrice.Sub(rate.IndexPrice).Div(rate.IndexPrice)

		// 基础利率 (假设 0.01%)
		baseRate := decimal.NewFromFloat(0.0001)

		// 资金费率 = 溢价 + 基础利率, 限制在 [-0.75%, 0.75%]
		fundingRate := premium.Add(baseRate)
		maxRate := decimal.NewFromFloat(0.0075)
		minRate := decimal.NewFromFloat(-0.0075)

		if fundingRate.GreaterThan(maxRate) {
			fundingRate = maxRate
		} else if fundingRate.LessThan(minRate) {
			fundingRate = minRate
		}

		rate.FundingRate = fundingRate
		s.fundingRates[symbol] = rate
	}
}

// settleAllSymbols 结算所有合约
func (s *Service) settleAllSymbols() {
	s.mu.RLock()
	symbols := make([]string, 0, len(s.fundingRates))
	for symbol := range s.fundingRates {
		symbols = append(symbols, symbol)
	}
	s.mu.RUnlock()

	for _, symbol := range symbols {
		s.SettleFunding(symbol)
	}
}

// SettleFunding 结算资金费用
func (s *Service) SettleFunding(symbol string) error {
	s.mu.Lock()
	rate, ok := s.fundingRates[symbol]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("funding rate not found for %s", symbol)
	}

	// 保存历史记录
	historyCopy := *rate
	historyCopy.FundingTime = time.Now().UnixNano()
	s.history[symbol] = append(s.history[symbol], &historyCopy)

	// 更新下次结算时间
	rate.NextFundingTime = time.Now().Add(s.defaultInterval).UnixNano()
	s.fundingRates[symbol] = rate
	currentRate := rate.FundingRate
	markPrice := rate.MarkPrice
	s.mu.Unlock()

	// 获取该合约所有仓位并结算
	// TODO: 这里需要遍历所有用户仓位，实际实现需要与 position manager 配合
	s.logger.Info("Funding settlement completed",
		zap.String("symbol", symbol),
		zap.String("rate", currentRate.String()),
		zap.String("mark_price", markPrice.String()),
	)

	return nil
}

// ProcessFundingPayment 处理单个仓位的资金费用
func (s *Service) ProcessFundingPayment(pos *types.Position, fundingRate decimal.Decimal) *types.FundingPayment {
	if pos.Quantity.IsZero() {
		return nil
	}

	// 资金费用 = 仓位价值 * 资金费率
	// 仓位价值 = 数量 * 标记价格
	positionValue := pos.Quantity.Mul(pos.MarkPrice)
	amount := positionValue.Mul(fundingRate)

	// 多头支付给空头 (当资金费率为正时)
	// 空头支付给多头 (当资金费率为负时)
	if pos.Side == types.PositionSideLong {
		amount = amount.Neg() // 多头支付为负
	}

	payment := &types.FundingPayment{
		PaymentID:   fmt.Sprintf("fp_%s_%d", pos.PositionID, time.Now().UnixNano()),
		UserID:      pos.UserID,
		Symbol:      pos.Symbol,
		PositionID:  pos.PositionID,
		FundingRate: fundingRate,
		Amount:      amount,
		PositionQty: pos.Quantity,
		Timestamp:   time.Now().UnixNano(),
	}

	// 发送到通道
	select {
	case s.paymentChan <- payment:
	default:
		s.logger.Warn("Funding payment channel full, dropping payment")
	}

	return payment
}

// UpdatePrices 更新标记价格和指数价格
func (s *Service) UpdatePrices(symbol string, markPrice, indexPrice decimal.Decimal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rate, ok := s.fundingRates[symbol]
	if !ok {
		rate = &types.FundingRate{
			Symbol:          symbol,
			NextFundingTime: s.getNextFundingTime(),
		}
	}

	rate.MarkPrice = markPrice
	rate.IndexPrice = indexPrice
	s.fundingRates[symbol] = rate
}

// getNextFundingTime 获取下一个资金费率结算时间
func (s *Service) getNextFundingTime() int64 {
	now := time.Now().UTC()
	hour := now.Hour()

	var nextHour int
	if hour < 8 {
		nextHour = 8
	} else if hour < 16 {
		nextHour = 16
	} else {
		nextHour = 24 // 明天0点
	}

	next := time.Date(now.Year(), now.Month(), now.Day(), nextHour, 0, 0, 0, time.UTC)
	if nextHour == 24 {
		next = next.Add(24 * time.Hour).Add(-24 * time.Hour) // 调整到明天0点
		next = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	}

	return next.UnixNano()
}

// GetFundingRate 获取当前资金费率
func (s *Service) GetFundingRate(symbol string) (*types.FundingRate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rate, ok := s.fundingRates[symbol]
	if !ok {
		return nil, false
	}
	rateCopy := *rate
	return &rateCopy, true
}

// GetFundingHistory 获取资金费率历史
func (s *Service) GetFundingHistory(symbol string, limit int) []*types.FundingRate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history, ok := s.history[symbol]
	if !ok {
		return nil
	}

	if limit <= 0 || limit > len(history) {
		limit = len(history)
	}

	// 返回最近的 limit 条
	start := len(history) - limit
	result := make([]*types.FundingRate, limit)
	for i, rate := range history[start:] {
		rateCopy := *rate
		result[i] = &rateCopy
	}
	return result
}

// PaymentChannel 返回资金费用支付通道
func (s *Service) PaymentChannel() <-chan *types.FundingPayment {
	return s.paymentChan
}

// AddInsuranceFund 增加保险基金
func (s *Service) AddInsuranceFund(symbol string, amount decimal.Decimal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fund, ok := s.insuranceFunds[symbol]
	if !ok {
		fund = &types.InsuranceFund{
			Symbol:  symbol,
			Balance: decimal.Zero,
		}
	}

	fund.Balance = fund.Balance.Add(amount)
	fund.Timestamp = time.Now().UnixNano()
	s.insuranceFunds[symbol] = fund
}

// GetInsuranceFund 获取保险基金余额
func (s *Service) GetInsuranceFund(symbol string) (*types.InsuranceFund, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fund, ok := s.insuranceFunds[symbol]
	return fund, ok
}

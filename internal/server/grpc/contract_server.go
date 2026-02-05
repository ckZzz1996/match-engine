package grpc

import (
	"context"
	"fmt"
	"time"

	pb "match-engine/api/proto"
	"match-engine/pkg/engine"
	"match-engine/pkg/funding"
	"match-engine/pkg/liquidation"
	"match-engine/pkg/position"
	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ContractServer 合约服务实现
type ContractServer struct {
	pb.UnimplementedContractServiceServer
	engineMgr      *engine.EngineManager
	positionMgr    *position.Manager
	liquidationEng *liquidation.Engine
	fundingSvc     *funding.Service
	logger         *zap.Logger
}

// NewContractServer 创建合约服务
func NewContractServer(
	engineMgr *engine.EngineManager,
	positionMgr *position.Manager,
	liquidationEng *liquidation.Engine,
	fundingSvc *funding.Service,
	logger *zap.Logger,
) *ContractServer {
	return &ContractServer{
		engineMgr:      engineMgr,
		positionMgr:    positionMgr,
		liquidationEng: liquidationEng,
		fundingSvc:     fundingSvc,
		logger:         logger,
	}
}

// CreateContractOrder 创建合约订单
func (s *ContractServer) CreateContractOrder(ctx context.Context, req *pb.CreateContractOrderRequest) (*pb.CreateContractOrderResponse, error) {
	// 参数验证
	if req.UserId == "" || req.Symbol == "" || req.Quantity == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	// 解析价格和数量
	price := decimal.Zero
	if req.Type == pb.OrderType_ORDER_TYPE_LIMIT && req.Price != "" {
		var err error
		price, err = decimal.NewFromString(req.Price)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid price")
		}
	}

	quantity, err := decimal.NewFromString(req.Quantity)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid quantity")
	}

	// 构建订单
	orderID := fmt.Sprintf("co_%d", time.Now().UnixNano())
	order := &types.ContractOrder{
		Order: &types.Order{
			OrderID:     orderID,
			UserID:      req.UserId,
			Symbol:      req.Symbol,
			Side:        convertOrderSide(req.Side),
			Type:        convertOrderType(req.Type),
			Price:       price,
			Quantity:    quantity,
			TimeInForce: convertTimeInForce(req.TimeInForce),
			Status:      types.OrderStatusNew,
			CreateTime:  time.Now().UnixNano(),
		},
		PositionSide:  convertPositionSide(req.PositionSide),
		Leverage:      int(req.Leverage),
		MarginType:    convertMarginType(req.MarginType),
		ReduceOnly:    req.ReduceOnly,
		ClosePosition: req.ClosePosition,
	}

	// 止损价
	if req.StopPrice != "" {
		stopPrice, err := decimal.NewFromString(req.StopPrice)
		if err == nil {
			order.StopPrice = stopPrice
		}
	}

	// 提交到合约撮合引擎
	result := s.engineMgr.ProcessContractOrder(order)
	if result.Rejected {
		s.logger.Warn("Contract order rejected",
			zap.String("order_id", orderID),
			zap.String("reason", result.RejectReason),
		)
		return &pb.CreateContractOrderResponse{
			Success: false,
			Message: result.RejectReason,
			Order:   convertContractOrderToPb(order),
		}, nil
	}

	s.logger.Info("Contract order created",
		zap.String("order_id", orderID),
		zap.String("user_id", req.UserId),
		zap.String("symbol", req.Symbol),
		zap.String("status", order.Status.String()),
	)

	return &pb.CreateContractOrderResponse{
		Success: true,
		Message: "order created",
		Order:   convertContractOrderToPb(order),
	}, nil
}

// ClosePosition 平仓
func (s *ContractServer) ClosePosition(ctx context.Context, req *pb.ClosePositionRequest) (*pb.ClosePositionResponse, error) {
	if req.UserId == "" || req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	positionSide := convertPositionSide(req.PositionSide)

	// 获取标记价格作为平仓价格
	markPrice, ok := s.liquidationEng.GetMarkPrice(req.Symbol)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "mark price not available")
	}

	// 平仓
	pos, realizedPnL, err := s.positionMgr.ClosePosition(req.UserId, req.Symbol, positionSide, markPrice)
	if err != nil {
		return &pb.ClosePositionResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	s.logger.Info("Position closed",
		zap.String("user_id", req.UserId),
		zap.String("symbol", req.Symbol),
		zap.String("realized_pnl", realizedPnL.String()),
	)

	return &pb.ClosePositionResponse{
		Success:     true,
		Message:     "position closed",
		Position:    convertPositionToPb(pos),
		RealizedPnl: realizedPnL.String(),
	}, nil
}

// GetPosition 获取仓位
func (s *ContractServer) GetPosition(ctx context.Context, req *pb.GetPositionRequest) (*pb.GetPositionResponse, error) {
	if req.UserId == "" || req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	positionSide := convertPositionSide(req.PositionSide)
	posKey := fmt.Sprintf("%s_%s", req.Symbol, positionSide.String())

	pos, ok := s.positionMgr.GetPosition(req.UserId, posKey)
	if !ok {
		return &pb.GetPositionResponse{
			Success:  true,
			Position: nil,
		}, nil
	}

	return &pb.GetPositionResponse{
		Success:  true,
		Position: convertPositionToPb(pos),
	}, nil
}

// GetUserPositions 获取用户所有仓位
func (s *ContractServer) GetUserPositions(ctx context.Context, req *pb.GetUserPositionsRequest) (*pb.GetUserPositionsResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "missing user_id")
	}

	positions := s.positionMgr.GetUserPositions(req.UserId)

	pbPositions := make([]*pb.Position, 0, len(positions))
	for _, pos := range positions {
		pbPositions = append(pbPositions, convertPositionToPb(pos))
	}

	return &pb.GetUserPositionsResponse{
		Success:   true,
		Positions: pbPositions,
	}, nil
}

// ChangeLeverage 修改杠杆
func (s *ContractServer) ChangeLeverage(ctx context.Context, req *pb.ChangeLeverageRequest) (*pb.ChangeLeverageResponse, error) {
	if req.UserId == "" || req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	if req.Leverage <= 0 || req.Leverage > 125 {
		return nil, status.Error(codes.InvalidArgument, "invalid leverage, must be 1-125")
	}

	// 获取合约配置
	contract, ok := s.positionMgr.GetContract(req.Symbol)
	if !ok {
		return nil, status.Error(codes.NotFound, "contract not found")
	}

	if int(req.Leverage) > contract.MaxLeverage {
		return &pb.ChangeLeverageResponse{
			Success: false,
			Message: fmt.Sprintf("leverage exceeds max: %d", contract.MaxLeverage),
		}, nil
	}

	// TODO: 更新用户杠杆设置
	// 需要检查当前仓位是否允许修改杠杆

	s.logger.Info("Leverage changed",
		zap.String("user_id", req.UserId),
		zap.String("symbol", req.Symbol),
		zap.Int32("leverage", req.Leverage),
	)

	return &pb.ChangeLeverageResponse{
		Success:  true,
		Message:  "leverage updated",
		Leverage: req.Leverage,
	}, nil
}

// ChangeMarginType 修改保证金模式
func (s *ContractServer) ChangeMarginType(ctx context.Context, req *pb.ChangeMarginTypeRequest) (*pb.ChangeMarginTypeResponse, error) {
	if req.UserId == "" || req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	// TODO: 检查是否有持仓，有持仓时不允许切换
	// 需要与账户系统集成

	s.logger.Info("Margin type changed",
		zap.String("user_id", req.UserId),
		zap.String("symbol", req.Symbol),
		zap.String("margin_type", req.MarginType.String()),
	)

	return &pb.ChangeMarginTypeResponse{
		Success: true,
		Message: "margin type updated",
	}, nil
}

// GetFundingRate 获取资金费率
func (s *ContractServer) GetFundingRate(ctx context.Context, req *pb.GetFundingRateRequest) (*pb.GetFundingRateResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "missing symbol")
	}

	rate, ok := s.fundingSvc.GetFundingRate(req.Symbol)
	if !ok {
		return &pb.GetFundingRateResponse{
			Success: false,
		}, nil
	}

	return &pb.GetFundingRateResponse{
		Success: true,
		FundingRate: &pb.FundingRate{
			Symbol:          rate.Symbol,
			FundingRate:     rate.FundingRate.String(),
			FundingTime:     rate.FundingTime,
			MarkPrice:       rate.MarkPrice.String(),
			IndexPrice:      rate.IndexPrice.String(),
			NextFundingTime: rate.NextFundingTime,
		},
	}, nil
}

// SubscribePositions 订阅仓位更新
func (s *ContractServer) SubscribePositions(req *pb.SubscribePositionsRequest, stream pb.ContractService_SubscribePositionsServer) error {
	if req.UserId == "" {
		return status.Error(codes.InvalidArgument, "missing user_id")
	}

	s.logger.Info("Position subscription started", zap.String("user_id", req.UserId))

	// 获取事件通道
	eventChan := s.engineMgr.EventChannel()

	for {
		select {
		case <-stream.Context().Done():
			s.logger.Info("Position subscription ended", zap.String("user_id", req.UserId))
			return nil
		case event, ok := <-eventChan:
			if !ok {
				return nil
			}

			if event.GetType() != types.EventTypePosition {
				continue
			}

			// 类型断言获取仓位更新
			posUpdate, ok := event.(*types.BaseEvent)
			if !ok {
				continue
			}

			// 检查是否是该用户的仓位
			// TODO: 需要完善事件数据结构

			if err := stream.Send(&pb.PositionUpdate{
				Timestamp: posUpdate.Timestamp,
			}); err != nil {
				return err
			}
		}
	}
}

// SubscribeLiquidations 订阅强平事件
func (s *ContractServer) SubscribeLiquidations(req *pb.SubscribeLiquidationsRequest, stream pb.ContractService_SubscribeLiquidationsServer) error {
	s.logger.Info("Liquidation subscription started", zap.String("symbol", req.Symbol))

	liquidationChan := s.liquidationEng.LiquidationChannel()

	for {
		select {
		case <-stream.Context().Done():
			s.logger.Info("Liquidation subscription ended")
			return nil
		case order, ok := <-liquidationChan:
			if !ok {
				return nil
			}

			// 过滤交易对
			if req.Symbol != "" && order.Symbol != req.Symbol {
				continue
			}

			var posSide pb.PositionSide
			if order.Side == types.OrderSideSell {
				posSide = pb.PositionSide_POSITION_SIDE_LONG
			} else {
				posSide = pb.PositionSide_POSITION_SIDE_SHORT
			}

			if err := stream.Send(&pb.LiquidationEvent{
				LiquidationId: order.LiquidationID,
				UserId:        order.UserID,
				Symbol:        order.Symbol,
				PositionSide:  posSide,
				Quantity:      order.Quantity.String(),
				Price:         order.Price.String(),
				LossAmount:    order.LossAmount.String(),
				Timestamp:     order.Timestamp,
			}); err != nil {
				return err
			}
		}
	}
}

// SetPositionTPSL 设置止盈止损
func (s *ContractServer) SetPositionTPSL(ctx context.Context, req *pb.SetPositionTPSLRequest) (*pb.SetPositionTPSLResponse, error) {
	if req.UserId == "" || req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	positionSide := convertPositionSide(req.PositionSide)

	// 解析止盈止损价格
	tpPrice := decimal.Zero
	slPrice := decimal.Zero

	if req.TakeProfitPrice != "" {
		var err error
		tpPrice, err = decimal.NewFromString(req.TakeProfitPrice)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid take profit price")
		}
	}

	if req.StopLossPrice != "" {
		var err error
		slPrice, err = decimal.NewFromString(req.StopLossPrice)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid stop loss price")
		}
	}

	// 验证仓位存在
	posKey := fmt.Sprintf("%s_%s", req.Symbol, positionSide.String())
	pos, ok := s.positionMgr.GetPosition(req.UserId, posKey)
	if !ok || pos.Quantity.IsZero() {
		return &pb.SetPositionTPSLResponse{
			Success: false,
			Message: "no position found",
		}, nil
	}

	// 验证止盈止损价格合理性
	markPrice, _ := s.liquidationEng.GetMarkPrice(req.Symbol)

	if pos.Side == types.PositionSideLong {
		// 多头: 止盈价应大于标记价，止损价应小于标记价
		if !tpPrice.IsZero() && tpPrice.LessThanOrEqual(markPrice) {
			return &pb.SetPositionTPSLResponse{
				Success: false,
				Message: "take profit price must be greater than mark price for long position",
			}, nil
		}
		if !slPrice.IsZero() && slPrice.GreaterThanOrEqual(markPrice) {
			return &pb.SetPositionTPSLResponse{
				Success: false,
				Message: "stop loss price must be less than mark price for long position",
			}, nil
		}
	} else {
		// 空头: 止盈价应小于标记价，止损价应大于标记价
		if !tpPrice.IsZero() && tpPrice.GreaterThanOrEqual(markPrice) {
			return &pb.SetPositionTPSLResponse{
				Success: false,
				Message: "take profit price must be less than mark price for short position",
			}, nil
		}
		if !slPrice.IsZero() && slPrice.LessThanOrEqual(markPrice) {
			return &pb.SetPositionTPSLResponse{
				Success: false,
				Message: "stop loss price must be greater than mark price for short position",
			}, nil
		}
	}

	// 获取合约引擎并设置止盈止损
	engine := s.engineMgr.GetContractEngine(req.Symbol)
	if engine == nil {
		return &pb.SetPositionTPSLResponse{
			Success: false,
			Message: "contract not found",
		}, nil
	}

	engine.SetPositionTPSL(req.UserId, req.Symbol, positionSide, tpPrice, slPrice)

	s.logger.Info("TPSL set",
		zap.String("user_id", req.UserId),
		zap.String("symbol", req.Symbol),
		zap.String("tp_price", tpPrice.String()),
		zap.String("sl_price", slPrice.String()),
	)

	return &pb.SetPositionTPSLResponse{
		Success: true,
		Message: "TPSL set successfully",
	}, nil
}

// GetPositionTPSL 获取止盈止损设置
func (s *ContractServer) GetPositionTPSL(ctx context.Context, req *pb.GetPositionTPSLRequest) (*pb.GetPositionTPSLResponse, error) {
	if req.UserId == "" || req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	positionSide := convertPositionSide(req.PositionSide)

	engine := s.engineMgr.GetContractEngine(req.Symbol)
	if engine == nil {
		return &pb.GetPositionTPSLResponse{
			Success: false,
		}, nil
	}

	tpsl := engine.GetPositionTPSL(req.UserId, req.Symbol, positionSide)
	if tpsl == nil {
		return &pb.GetPositionTPSLResponse{
			Success:         true,
			TakeProfitPrice: "",
			StopLossPrice:   "",
		}, nil
	}

	return &pb.GetPositionTPSLResponse{
		Success:         true,
		TakeProfitPrice: tpsl.TakeProfitPrice.String(),
		StopLossPrice:   tpsl.StopLossPrice.String(),
	}, nil
}

// CancelPositionTPSL 取消止盈止损
func (s *ContractServer) CancelPositionTPSL(ctx context.Context, req *pb.CancelPositionTPSLRequest) (*pb.CancelPositionTPSLResponse, error) {
	if req.UserId == "" || req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	positionSide := convertPositionSide(req.PositionSide)

	engine := s.engineMgr.GetContractEngine(req.Symbol)
	if engine == nil {
		return &pb.CancelPositionTPSLResponse{
			Success: false,
			Message: "contract not found",
		}, nil
	}

	// 设置为零价格即为取消
	engine.SetPositionTPSL(req.UserId, req.Symbol, positionSide, decimal.Zero, decimal.Zero)

	s.logger.Info("TPSL canceled",
		zap.String("user_id", req.UserId),
		zap.String("symbol", req.Symbol),
	)

	return &pb.CancelPositionTPSLResponse{
		Success: true,
		Message: "TPSL canceled",
	}, nil
}

// GetMarkPrice 获取标记价格
func (s *ContractServer) GetMarkPrice(ctx context.Context, req *pb.GetMarkPriceRequest) (*pb.GetMarkPriceResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "missing symbol")
	}

	markPrice, ok := s.liquidationEng.GetMarkPrice(req.Symbol)
	if !ok {
		return &pb.GetMarkPriceResponse{
			Success: false,
		}, nil
	}

	rate, _ := s.fundingSvc.GetFundingRate(req.Symbol)

	info := &pb.MarkPriceInfo{
		Symbol:    req.Symbol,
		MarkPrice: markPrice.String(),
		Timestamp: time.Now().UnixNano(),
	}

	if rate != nil {
		info.IndexPrice = rate.IndexPrice.String()
		info.FundingRate = rate.FundingRate.String()
		info.NextFundingTime = rate.NextFundingTime
	}

	return &pb.GetMarkPriceResponse{
		Success:       true,
		MarkPriceInfo: info,
	}, nil
}

// AdjustMargin 调整保证金
func (s *ContractServer) AdjustMargin(ctx context.Context, req *pb.AdjustMarginRequest) (*pb.AdjustMarginResponse, error) {
	if req.UserId == "" || req.Symbol == "" || req.Amount == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid amount")
	}

	positionSide := convertPositionSide(req.PositionSide)
	posKey := fmt.Sprintf("%s_%s", req.Symbol, positionSide.String())

	pos, ok := s.positionMgr.GetPosition(req.UserId, posKey)
	if !ok || pos.Quantity.IsZero() {
		return &pb.AdjustMarginResponse{
			Success: false,
			Message: "no position found",
		}, nil
	}

	// 只有逐仓模式可以调整保证金
	if pos.MarginType != types.MarginTypeIsolated {
		return &pb.AdjustMarginResponse{
			Success: false,
			Message: "only isolated margin can be adjusted",
		}, nil
	}

	// 调整保证金
	newMargin := pos.Margin.Add(amount)
	if newMargin.LessThanOrEqual(decimal.Zero) {
		return &pb.AdjustMarginResponse{
			Success: false,
			Message: "insufficient margin",
		}, nil
	}

	// TODO: 实际调整保证金逻辑，需要与账户系统集成

	s.logger.Info("Margin adjusted",
		zap.String("user_id", req.UserId),
		zap.String("symbol", req.Symbol),
		zap.String("amount", amount.String()),
	)

	return &pb.AdjustMarginResponse{
		Success: true,
		Message: "margin adjusted",
		Margin:  newMargin.String(),
	}, nil
}

// GetInsuranceFund 获取保险基金
func (s *ContractServer) GetInsuranceFund(ctx context.Context, req *pb.GetInsuranceFundRequest) (*pb.GetInsuranceFundResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "missing symbol")
	}

	fund, ok := s.fundingSvc.GetInsuranceFund(req.Symbol)
	if !ok {
		return &pb.GetInsuranceFundResponse{
			Success: true,
			Symbol:  req.Symbol,
			Balance: "0",
		}, nil
	}

	return &pb.GetInsuranceFundResponse{
		Success:   true,
		Symbol:    fund.Symbol,
		Balance:   fund.Balance.String(),
		Timestamp: fund.Timestamp,
	}, nil
}

// GetFundingRateHistory 获取资金费率历史
func (s *ContractServer) GetFundingRateHistory(ctx context.Context, req *pb.GetFundingRateHistoryRequest) (*pb.GetFundingRateHistoryResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "missing symbol")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100
	}

	history := s.fundingSvc.GetFundingHistory(req.Symbol, limit)

	pbHistory := make([]*pb.FundingRate, 0, len(history))
	for _, rate := range history {
		pbHistory = append(pbHistory, &pb.FundingRate{
			Symbol:          rate.Symbol,
			FundingRate:     rate.FundingRate.String(),
			FundingTime:     rate.FundingTime,
			MarkPrice:       rate.MarkPrice.String(),
			IndexPrice:      rate.IndexPrice.String(),
			NextFundingTime: rate.NextFundingTime,
		})
	}

	return &pb.GetFundingRateHistoryResponse{
		Success: true,
		History: pbHistory,
	}, nil
}

// 辅助函数: 转换仓位方向
func convertPositionSide(side pb.PositionSide) types.PositionSide {
	switch side {
	case pb.PositionSide_POSITION_SIDE_LONG:
		return types.PositionSideLong
	case pb.PositionSide_POSITION_SIDE_SHORT:
		return types.PositionSideShort
	case pb.PositionSide_POSITION_SIDE_BOTH:
		return types.PositionSideBoth
	default:
		return types.PositionSideBoth
	}
}

// 辅助函数: 转换保证金类型
func convertMarginType(mt pb.MarginType) types.MarginType {
	switch mt {
	case pb.MarginType_MARGIN_TYPE_CROSS:
		return types.MarginTypeCross
	case pb.MarginType_MARGIN_TYPE_ISOLATED:
		return types.MarginTypeIsolated
	default:
		return types.MarginTypeCross
	}
}

// 辅助函数: Position 转 pb
func convertPositionToPb(pos *types.Position) *pb.Position {
	return &pb.Position{
		PositionId:       pos.PositionID,
		UserId:           pos.UserID,
		Symbol:           pos.Symbol,
		Side:             convertPositionSideToPb(pos.Side),
		MarginType:       convertMarginTypeToPb(pos.MarginType),
		Leverage:         int32(pos.Leverage),
		EntryPrice:       pos.EntryPrice.String(),
		MarkPrice:        pos.MarkPrice.String(),
		LiquidationPrice: pos.LiquidationPrice.String(),
		Quantity:         pos.Quantity.String(),
		Margin:           pos.Margin.String(),
		UnrealizedPnl:    pos.UnrealizedPnL.String(),
		RealizedPnl:      pos.RealizedPnL.String(),
		CreateTime:       pos.CreateTime,
		UpdateTime:       pos.UpdateTime,
	}
}

func convertPositionSideToPb(side types.PositionSide) pb.PositionSide {
	switch side {
	case types.PositionSideLong:
		return pb.PositionSide_POSITION_SIDE_LONG
	case types.PositionSideShort:
		return pb.PositionSide_POSITION_SIDE_SHORT
	default:
		return pb.PositionSide_POSITION_SIDE_BOTH
	}
}

func convertMarginTypeToPb(mt types.MarginType) pb.MarginType {
	switch mt {
	case types.MarginTypeCross:
		return pb.MarginType_MARGIN_TYPE_CROSS
	case types.MarginTypeIsolated:
		return pb.MarginType_MARGIN_TYPE_ISOLATED
	default:
		return pb.MarginType_MARGIN_TYPE_CROSS
	}
}

// 辅助函数: ContractOrder 转 pb
func convertContractOrderToPb(order *types.ContractOrder) *pb.ContractOrder {
	return &pb.ContractOrder{
		OrderId:       order.OrderID,
		UserId:        order.UserID,
		Symbol:        order.Symbol,
		Side:          convertOrderSideToProto(order.Side),
		Type:          convertOrderTypeToProto(order.Type),
		Price:         order.Price.String(),
		Quantity:      order.Quantity.String(),
		TimeInForce:   convertTimeInForceToProto(order.TimeInForce),
		PositionSide:  convertPositionSideToPb(order.PositionSide),
		Leverage:      int32(order.Leverage),
		MarginType:    convertMarginTypeToPb(order.MarginType),
		ReduceOnly:    order.ReduceOnly,
		ClosePosition: order.ClosePosition,
		StopPrice:     order.StopPrice.String(),
		Status:        convertOrderStatusToProto(order.Status),
		FilledQty:     order.FilledQty.String(),
		CreateTime:    order.CreateTime,
		UpdateTime:    order.UpdateTime,
	}
}

package grpc

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "match-engine/api/proto"
	"match-engine/pkg/engine"
	"match-engine/pkg/types"
)

// OrderServer 订单服务实现
type OrderServer struct {
	pb.UnimplementedOrderServiceServer
	manager *engine.EngineManager
}

// NewOrderServer 创建订单服务
func NewOrderServer(manager *engine.EngineManager) *OrderServer {
	return &OrderServer{
		manager: manager,
	}
}

// CreateOrder 创建订单
func (s *OrderServer) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.CreateOrderResponse, error) {
	// 验证请求
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	// 解析价格和数量
	price := decimal.Zero
	if req.Price != "" {
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

	// 创建订单
	orderID := uuid.New().String()
	if req.ClientOrderId != "" {
		orderID = req.ClientOrderId
	}

	order := &types.Order{
		OrderID:     orderID,
		UserID:      req.UserId,
		Symbol:      req.Symbol,
		Side:        convertOrderSide(req.Side),
		Type:        convertOrderType(req.Type),
		Price:       price,
		Quantity:    quantity,
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: convertTimeInForce(req.TimeInForce),
		CreateTime:  time.Now().UnixNano(),
		UpdateTime:  time.Now().UnixNano(),
	}

	// 处理止损价
	if req.StopPrice != "" {
		stopPrice, err := decimal.NewFromString(req.StopPrice)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid stop_price")
		}
		order.StopPrice = stopPrice
	}

	// 处理冰山单可见数量
	if req.VisibleQty != "" {
		visibleQty, err := decimal.NewFromString(req.VisibleQty)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid visible_qty")
		}
		order.VisibleQty = visibleQty
	}

	order.ReduceOnly = req.ReduceOnly

	// 执行撮合
	result := s.manager.ProcessOrder(order)

	// 构建响应
	resp := &pb.CreateOrderResponse{
		Success: !result.Rejected,
		Message: result.RejectReason,
		Order:   convertOrderToProto(result.TakerOrder),
		Trades:  make([]*pb.Trade, 0, len(result.Trades)),
	}

	for _, trade := range result.Trades {
		resp.Trades = append(resp.Trades, convertTradeToProto(trade))
	}

	return resp, nil
}

// CancelOrder 取消订单
func (s *OrderServer) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	if req.Symbol == "" || req.OrderId == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol and order_id are required")
	}

	order, err := s.manager.CancelOrder(req.Symbol, req.OrderId)
	if err != nil {
		return &pb.CancelOrderResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	return &pb.CancelOrderResponse{
		Success: true,
		Order:   convertOrderToProto(order),
	}, nil
}

// GetOrder 获取订单
func (s *OrderServer) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.GetOrderResponse, error) {
	if req.Symbol == "" || req.OrderId == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol and order_id are required")
	}

	order := s.manager.GetOrder(req.Symbol, req.OrderId)
	if order == nil {
		return &pb.GetOrderResponse{
			Success: false,
		}, nil
	}

	return &pb.GetOrderResponse{
		Success: true,
		Order:   convertOrderToProto(order),
	}, nil
}

// GetUserOrders 获取用户订单
func (s *OrderServer) GetUserOrders(ctx context.Context, req *pb.GetUserOrdersRequest) (*pb.GetUserOrdersResponse, error) {
	if req.Symbol == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol and user_id are required")
	}

	orders := s.manager.GetUserOrders(req.Symbol, req.UserId)

	protoOrders := make([]*pb.Order, 0, len(orders))
	for _, order := range orders {
		protoOrders = append(protoOrders, convertOrderToProto(order))
	}

	return &pb.GetUserOrdersResponse{
		Success: true,
		Orders:  protoOrders,
	}, nil
}

// BatchCreateOrders 批量创建订单
func (s *OrderServer) BatchCreateOrders(ctx context.Context, req *pb.BatchCreateOrdersRequest) (*pb.BatchCreateOrdersResponse, error) {
	results := make([]*pb.CreateOrderResponse, 0, len(req.Orders))

	for _, orderReq := range req.Orders {
		resp, err := s.CreateOrder(ctx, orderReq)
		if err != nil {
			results = append(results, &pb.CreateOrderResponse{
				Success: false,
				Message: err.Error(),
			})
		} else {
			results = append(results, resp)
		}
	}

	return &pb.BatchCreateOrdersResponse{
		Success: true,
		Results: results,
	}, nil
}

// BatchCancelOrders 批量取消订单
func (s *OrderServer) BatchCancelOrders(ctx context.Context, req *pb.BatchCancelOrdersRequest) (*pb.BatchCancelOrdersResponse, error) {
	results := make([]*pb.CancelOrderResponse, 0, len(req.OrderIds))

	for _, orderID := range req.OrderIds {
		resp, err := s.CancelOrder(ctx, &pb.CancelOrderRequest{
			Symbol:  req.Symbol,
			OrderId: orderID,
			UserId:  req.UserId,
		})
		if err != nil {
			results = append(results, &pb.CancelOrderResponse{
				Success: false,
				Message: err.Error(),
			})
		} else {
			results = append(results, resp)
		}
	}

	return &pb.BatchCancelOrdersResponse{
		Success: true,
		Results: results,
	}, nil
}

// Register 注册到gRPC服务器
func (s *OrderServer) Register(server *grpc.Server) {
	pb.RegisterOrderServiceServer(server, s)
}

// 转换函数
func convertOrderSide(side pb.OrderSide) types.OrderSide {
	switch side {
	case pb.OrderSide_ORDER_SIDE_BUY:
		return types.OrderSideBuy
	case pb.OrderSide_ORDER_SIDE_SELL:
		return types.OrderSideSell
	default:
		return types.OrderSideBuy
	}
}

func convertOrderType(t pb.OrderType) types.OrderType {
	switch t {
	case pb.OrderType_ORDER_TYPE_LIMIT:
		return types.OrderTypeLimit
	case pb.OrderType_ORDER_TYPE_MARKET:
		return types.OrderTypeMarket
	case pb.OrderType_ORDER_TYPE_STOP_LOSS:
		return types.OrderTypeStopLoss
	case pb.OrderType_ORDER_TYPE_TAKE_PROFIT:
		return types.OrderTypeTakeProfit
	case pb.OrderType_ORDER_TYPE_ICEBERG:
		return types.OrderTypeIceberg
	default:
		return types.OrderTypeLimit
	}
}

func convertTimeInForce(tif pb.TimeInForce) types.TimeInForce {
	switch tif {
	case pb.TimeInForce_TIME_IN_FORCE_GTC:
		return types.TimeInForceGTC
	case pb.TimeInForce_TIME_IN_FORCE_IOC:
		return types.TimeInForceIOC
	case pb.TimeInForce_TIME_IN_FORCE_FOK:
		return types.TimeInForceFOK
	case pb.TimeInForce_TIME_IN_FORCE_POST_ONLY:
		return types.TimeInForcePostOnly
	default:
		return types.TimeInForceGTC
	}
}

func convertOrderToProto(order *types.Order) *pb.Order {
	if order == nil {
		return nil
	}
	return &pb.Order{
		OrderId:     order.OrderID,
		UserId:      order.UserID,
		Symbol:      order.Symbol,
		Side:        convertOrderSideToProto(order.Side),
		Type:        convertOrderTypeToProto(order.Type),
		Price:       order.Price.String(),
		Quantity:    order.Quantity.String(),
		FilledQty:   order.FilledQty.String(),
		Status:      convertOrderStatusToProto(order.Status),
		TimeInForce: convertTimeInForceToProto(order.TimeInForce),
		StopPrice:   order.StopPrice.String(),
		VisibleQty:  order.VisibleQty.String(),
		ReduceOnly:  order.ReduceOnly,
		CreateTime:  order.CreateTime,
		UpdateTime:  order.UpdateTime,
	}
}

func convertTradeToProto(trade *types.Trade) *pb.Trade {
	if trade == nil {
		return nil
	}
	return &pb.Trade{
		TradeId:      trade.TradeID,
		Symbol:       trade.Symbol,
		MakerOrderId: trade.MakerOrderID,
		TakerOrderId: trade.TakerOrderID,
		MakerUserId:  trade.MakerUserID,
		TakerUserId:  trade.TakerUserID,
		Price:        trade.Price.String(),
		Quantity:     trade.Quantity.String(),
		MakerSide:    convertOrderSideToProto(trade.MakerSide),
		TakerSide:    convertOrderSideToProto(trade.TakerSide),
		TradeTime:    trade.TradeTime,
	}
}

func convertOrderSideToProto(side types.OrderSide) pb.OrderSide {
	switch side {
	case types.OrderSideBuy:
		return pb.OrderSide_ORDER_SIDE_BUY
	case types.OrderSideSell:
		return pb.OrderSide_ORDER_SIDE_SELL
	default:
		return pb.OrderSide_ORDER_SIDE_UNSPECIFIED
	}
}

func convertOrderTypeToProto(t types.OrderType) pb.OrderType {
	switch t {
	case types.OrderTypeLimit:
		return pb.OrderType_ORDER_TYPE_LIMIT
	case types.OrderTypeMarket:
		return pb.OrderType_ORDER_TYPE_MARKET
	case types.OrderTypeStopLoss:
		return pb.OrderType_ORDER_TYPE_STOP_LOSS
	case types.OrderTypeTakeProfit:
		return pb.OrderType_ORDER_TYPE_TAKE_PROFIT
	case types.OrderTypeIceberg:
		return pb.OrderType_ORDER_TYPE_ICEBERG
	default:
		return pb.OrderType_ORDER_TYPE_UNSPECIFIED
	}
}

func convertTimeInForceToProto(tif types.TimeInForce) pb.TimeInForce {
	switch tif {
	case types.TimeInForceGTC:
		return pb.TimeInForce_TIME_IN_FORCE_GTC
	case types.TimeInForceIOC:
		return pb.TimeInForce_TIME_IN_FORCE_IOC
	case types.TimeInForceFOK:
		return pb.TimeInForce_TIME_IN_FORCE_FOK
	case types.TimeInForcePostOnly:
		return pb.TimeInForce_TIME_IN_FORCE_POST_ONLY
	default:
		return pb.TimeInForce_TIME_IN_FORCE_UNSPECIFIED
	}
}

func convertOrderStatusToProto(s types.OrderStatus) pb.OrderStatus {
	switch s {
	case types.OrderStatusNew:
		return pb.OrderStatus_ORDER_STATUS_NEW
	case types.OrderStatusPartiallyFilled:
		return pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
	case types.OrderStatusFilled:
		return pb.OrderStatus_ORDER_STATUS_FILLED
	case types.OrderStatusCanceled:
		return pb.OrderStatus_ORDER_STATUS_CANCELED
	case types.OrderStatusRejected:
		return pb.OrderStatus_ORDER_STATUS_REJECTED
	case types.OrderStatusExpired:
		return pb.OrderStatus_ORDER_STATUS_EXPIRED
	default:
		return pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

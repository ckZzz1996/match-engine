package grpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "match-engine/api/proto"
	"match-engine/pkg/engine"
	"match-engine/pkg/types"
)

// MarketDataServer 行情服务实现
type MarketDataServer struct {
	pb.UnimplementedMarketDataServiceServer
	manager *engine.EngineManager
}

// NewMarketDataServer 创建行情服务
func NewMarketDataServer(manager *engine.EngineManager) *MarketDataServer {
	return &MarketDataServer{
		manager: manager,
	}
}

// GetOrderBook 获取订单簿快照
func (s *MarketDataServer) GetOrderBook(ctx context.Context, req *pb.GetOrderBookRequest) (*pb.GetOrderBookResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}

	depth := int(req.Depth)
	if depth <= 0 {
		depth = 20
	}

	snapshot := s.manager.Snapshot(req.Symbol, depth)
	if snapshot == nil {
		return &pb.GetOrderBookResponse{
			Success: false,
		}, nil
	}

	resp := &pb.GetOrderBookResponse{
		Success:    true,
		Symbol:     snapshot.Symbol,
		Bids:       make([]*pb.PriceLevel, 0, len(snapshot.Bids)),
		Asks:       make([]*pb.PriceLevel, 0, len(snapshot.Asks)),
		LastPrice:  snapshot.LastPrice.String(),
		Timestamp:  snapshot.Timestamp,
		SequenceId: snapshot.SequenceID,
	}

	for _, level := range snapshot.Bids {
		resp.Bids = append(resp.Bids, &pb.PriceLevel{
			Price:      level.Price.String(),
			Quantity:   level.Quantity.String(),
			OrderCount: int32(level.Count),
		})
	}

	for _, level := range snapshot.Asks {
		resp.Asks = append(resp.Asks, &pb.PriceLevel{
			Price:      level.Price.String(),
			Quantity:   level.Quantity.String(),
			OrderCount: int32(level.Count),
		})
	}

	return resp, nil
}

// SubscribeOrderBook 订阅订单簿更新
func (s *MarketDataServer) SubscribeOrderBook(req *pb.SubscribeOrderBookRequest, stream pb.MarketDataService_SubscribeOrderBookServer) error {
	if req.Symbol == "" {
		return status.Error(codes.InvalidArgument, "symbol is required")
	}

	// 获取引擎
	eng := s.manager.GetEngine(req.Symbol)
	if eng == nil {
		return status.Error(codes.NotFound, "symbol not found")
	}

	// 监听事件通道
	eventChan := s.manager.EventChannel()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case event := <-eventChan:
			if event.GetSymbol() != req.Symbol {
				continue
			}

			// 只处理订单簿更新事件
			if event.GetType() == 6 { // EventTypeBookUpdate
				if bookEvent, ok := event.(*types.BookUpdateEvent); ok {
					for _, delta := range bookEvent.Deltas {
						update := &pb.OrderBookUpdate{
							Symbol:    delta.Symbol,
							Side:      convertOrderSideToProto(delta.Side),
							Price:     delta.Price.String(),
							Quantity:  delta.Quantity.String(),
							Action:    deltaActionToString(delta.Action),
							Timestamp: delta.Timestamp,
						}
						if err := stream.Send(update); err != nil {
							return err
						}
					}
				}
			}
		}
	}
}

// GetRecentTrades 获取最近成交
func (s *MarketDataServer) GetRecentTrades(ctx context.Context, req *pb.GetRecentTradesRequest) (*pb.GetRecentTradesResponse, error) {
	// TODO: 实现成交记录存储和查询
	return &pb.GetRecentTradesResponse{
		Success: true,
		Trades:  []*pb.Trade{},
	}, nil
}

// SubscribeTrades 订阅成交
func (s *MarketDataServer) SubscribeTrades(req *pb.SubscribeTradesRequest, stream pb.MarketDataService_SubscribeTradesServer) error {
	if req.Symbol == "" {
		return status.Error(codes.InvalidArgument, "symbol is required")
	}

	// 监听事件通道
	eventChan := s.manager.EventChannel()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case event := <-eventChan:
			if event.GetSymbol() != req.Symbol {
				continue
			}

			// 只处理成交事件
			if event.GetType() == 5 { // EventTypeTrade
				if tradeEvent, ok := event.(*types.TradeEvent); ok {
					if err := stream.Send(convertTradeToProto(tradeEvent.Trade)); err != nil {
						return err
					}
				}
			}
		}
	}
}

// GetTicker 获取行情数据
func (s *MarketDataServer) GetTicker(ctx context.Context, req *pb.GetTickerRequest) (*pb.GetTickerResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}

	eng := s.manager.GetEngine(req.Symbol)
	if eng == nil {
		return &pb.GetTickerResponse{
			Success: false,
		}, nil
	}

	ob := eng.OrderBook()

	return &pb.GetTickerResponse{
		Success:   true,
		Symbol:    req.Symbol,
		LastPrice: ob.GetLastPrice().String(),
		BestBid:   ob.BestBidPrice().String(),
		BestAsk:   ob.BestAskPrice().String(),
		// TODO: 实现更多统计数据
	}, nil
}

// Register 注册到gRPC服务器
func (s *MarketDataServer) Register(server *grpc.Server) {
	pb.RegisterMarketDataServiceServer(server, s)
}

func deltaActionToString(action types.DeltaAction) string {
	switch action {
	case types.DeltaActionAdd:
		return "ADD"
	case types.DeltaActionUpdate:
		return "UPDATE"
	case types.DeltaActionDelete:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

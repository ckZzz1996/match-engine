package grpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "match-engine/api/proto"
	"match-engine/pkg/engine"
)

// AdminServer 管理服务实现
type AdminServer struct {
	pb.UnimplementedAdminServiceServer
	manager *engine.EngineManager
}

// NewAdminServer 创建管理服务
func NewAdminServer(manager *engine.EngineManager) *AdminServer {
	return &AdminServer{
		manager: manager,
	}
}

// AddSymbol 添加交易对
func (s *AdminServer) AddSymbol(ctx context.Context, req *pb.AddSymbolRequest) (*pb.AddSymbolResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}

	s.manager.AddSymbol(req.Symbol)

	return &pb.AddSymbolResponse{
		Success: true,
		Message: "symbol added: " + req.Symbol,
	}, nil
}

// RemoveSymbol 移除交易对
func (s *AdminServer) RemoveSymbol(ctx context.Context, req *pb.RemoveSymbolRequest) (*pb.RemoveSymbolResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}

	s.manager.RemoveSymbol(req.Symbol)

	return &pb.RemoveSymbolResponse{
		Success: true,
		Message: "symbol removed: " + req.Symbol,
	}, nil
}

// PauseTrading 暂停交易
func (s *AdminServer) PauseTrading(ctx context.Context, req *pb.PauseTradingRequest) (*pb.PauseTradingResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}

	if !s.manager.PauseSymbol(req.Symbol) {
		return &pb.PauseTradingResponse{
			Success: false,
			Message: "symbol not found: " + req.Symbol,
		}, nil
	}

	return &pb.PauseTradingResponse{
		Success: true,
		Message: "trading paused for: " + req.Symbol,
	}, nil
}

// ResumeTrading 恢复交易
func (s *AdminServer) ResumeTrading(ctx context.Context, req *pb.ResumeTradingRequest) (*pb.ResumeTradingResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}

	if !s.manager.ResumeSymbol(req.Symbol) {
		return &pb.ResumeTradingResponse{
			Success: false,
			Message: "symbol not found: " + req.Symbol,
		}, nil
	}

	return &pb.ResumeTradingResponse{
		Success: true,
		Message: "trading resumed for: " + req.Symbol,
	}, nil
}

// StartAuction 开始集合竞价
func (s *AdminServer) StartAuction(ctx context.Context, req *pb.StartAuctionRequest) (*pb.StartAuctionResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}

	if !s.manager.StartAuction(req.Symbol) {
		return &pb.StartAuctionResponse{
			Success: false,
			Message: "failed to start auction for: " + req.Symbol,
		}, nil
	}

	return &pb.StartAuctionResponse{
		Success: true,
		Message: "auction started for: " + req.Symbol,
	}, nil
}

// EndAuction 结束集合竞价
func (s *AdminServer) EndAuction(ctx context.Context, req *pb.EndAuctionRequest) (*pb.EndAuctionResponse, error) {
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}

	result, err := s.manager.EndAuction(req.Symbol)
	if err != nil {
		return &pb.EndAuctionResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	return &pb.EndAuctionResponse{
		Success:    true,
		Message:    "auction ended for: " + req.Symbol,
		OpenPrice:  result.OpenPrice.String(),
		MaxVolume:  result.MaxVolume.String(),
		TradeCount: int32(len(result.Trades)),
	}, nil
}

// GetSystemStatus 获取系统状态
func (s *AdminServer) GetSystemStatus(ctx context.Context, req *pb.GetSystemStatusRequest) (*pb.GetSystemStatusResponse, error) {
	stats := s.manager.Stats()

	resp := &pb.GetSystemStatusResponse{
		Success:         true,
		Status:          "running",
		SymbolCount:     int32(stats["symbol_count"].(int)),
		EventQueueLen:   int64(stats["event_queue_len"].(int)),
		CommandQueueLen: int64(stats["command_queue_len"].(int)),
		Symbols:         make(map[string]*pb.SymbolStatus),
	}

	if symbolStats, ok := stats["symbols"].(map[string]interface{}); ok {
		for symbol, stat := range symbolStats {
			if statMap, ok := stat.(map[string]interface{}); ok {
				eng := s.manager.GetEngine(symbol)
				paused := false
				if eng != nil {
					paused = eng.IsPaused()
				}

				resp.Symbols[symbol] = &pb.SymbolStatus{
					Symbol:      symbol,
					BidLevels:   int32(statMap["bid_levels"].(int)),
					AskLevels:   int32(statMap["ask_levels"].(int)),
					TotalOrders: int32(statMap["total_orders"].(int)),
					LastPrice:   statMap["last_price"].(string),
					Paused:      paused,
				}
			}
		}
	}

	return resp, nil
}

// Register 注册到gRPC服务器
func (s *AdminServer) Register(server *grpc.Server) {
	pb.RegisterAdminServiceServer(server, s)
}

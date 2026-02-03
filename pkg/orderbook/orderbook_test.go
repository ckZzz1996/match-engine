package orderbook

import (
	"testing"

	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
)

func TestOrderBook_AddRemoveOrder(t *testing.T) {
	ob := NewOrderBook("BTC-USDT")

	order := &types.Order{
		OrderID:   "order-1",
		UserID:    "user-1",
		Symbol:    "BTC-USDT",
		Side:      types.OrderSideBuy,
		Type:      types.OrderTypeLimit,
		Price:     decimal.NewFromInt(50000),
		Quantity:  decimal.NewFromInt(1),
		FilledQty: decimal.Zero,
		Status:    types.OrderStatusNew,
	}

	ob.AddOrder(order)

	// 验证订单已添加
	got := ob.GetOrder("order-1")
	if got == nil {
		t.Error("order should exist in orderbook")
	}

	// 验证最优买价
	bestBid := ob.BestBid()
	if bestBid == nil {
		t.Error("best bid should exist")
	}
	if !bestBid.Price.Equal(decimal.NewFromInt(50000)) {
		t.Errorf("expected best bid 50000, got %s", bestBid.Price)
	}

	// 移除订单
	removed := ob.RemoveOrder("order-1")
	if removed == nil {
		t.Error("order should be removed")
	}

	// 验证订单已移除
	got = ob.GetOrder("order-1")
	if got != nil {
		t.Error("order should not exist after removal")
	}
}

func TestOrderBook_PricePriority(t *testing.T) {
	ob := NewOrderBook("BTC-USDT")

	// 添加多个买单
	prices := []int64{50000, 50100, 49900, 50200, 49800}
	for i, price := range prices {
		order := &types.Order{
			OrderID:   "order-" + string(rune('0'+i)),
			UserID:    "user-1",
			Symbol:    "BTC-USDT",
			Side:      types.OrderSideBuy,
			Type:      types.OrderTypeLimit,
			Price:     decimal.NewFromInt(price),
			Quantity:  decimal.NewFromInt(1),
			FilledQty: decimal.Zero,
			Status:    types.OrderStatusNew,
		}
		ob.AddOrder(order)
	}

	// 最优买价应该是最高价
	bestBid := ob.BestBid()
	if !bestBid.Price.Equal(decimal.NewFromInt(50200)) {
		t.Errorf("expected best bid 50200, got %s", bestBid.Price)
	}

	// 添加多个卖单
	sellPrices := []int64{50500, 50300, 50700, 50400, 50600}
	for i, price := range sellPrices {
		order := &types.Order{
			OrderID:   "sell-" + string(rune('0'+i)),
			UserID:    "user-1",
			Symbol:    "BTC-USDT",
			Side:      types.OrderSideSell,
			Type:      types.OrderTypeLimit,
			Price:     decimal.NewFromInt(price),
			Quantity:  decimal.NewFromInt(1),
			FilledQty: decimal.Zero,
			Status:    types.OrderStatusNew,
		}
		ob.AddOrder(order)
	}

	// 最优卖价应该是最低价
	bestAsk := ob.BestAsk()
	if !bestAsk.Price.Equal(decimal.NewFromInt(50300)) {
		t.Errorf("expected best ask 50300, got %s", bestAsk.Price)
	}
}

func TestOrderBook_TimePriority(t *testing.T) {
	ob := NewOrderBook("BTC-USDT")

	// 添加多个相同价格的买单
	for i := 0; i < 5; i++ {
		order := &types.Order{
			OrderID:    "order-" + string(rune('0'+i)),
			UserID:     "user-1",
			Symbol:     "BTC-USDT",
			Side:       types.OrderSideBuy,
			Type:       types.OrderTypeLimit,
			Price:      decimal.NewFromInt(50000),
			Quantity:   decimal.NewFromInt(1),
			FilledQty:  decimal.Zero,
			Status:     types.OrderStatusNew,
			SequenceID: int64(i),
		}
		ob.AddOrder(order)
	}

	// 获取该价格档位
	bestBid := ob.BestBid()
	if bestBid.Len() != 5 {
		t.Errorf("expected 5 orders, got %d", bestBid.Len())
	}

	// 队首应该是第一个订单（FIFO）
	front := bestBid.Front()
	if front.OrderID != "order-0" {
		t.Errorf("expected first order, got %s", front.OrderID)
	}
}

func TestOrderBook_Snapshot(t *testing.T) {
	ob := NewOrderBook("BTC-USDT")

	// 添加买卖单
	for i := 0; i < 10; i++ {
		buyOrder := &types.Order{
			OrderID:   "buy-" + string(rune('0'+i)),
			UserID:    "user-1",
			Symbol:    "BTC-USDT",
			Side:      types.OrderSideBuy,
			Type:      types.OrderTypeLimit,
			Price:     decimal.NewFromInt(int64(50000 - i*100)),
			Quantity:  decimal.NewFromInt(1),
			FilledQty: decimal.Zero,
			Status:    types.OrderStatusNew,
		}
		ob.AddOrder(buyOrder)

		sellOrder := &types.Order{
			OrderID:   "sell-" + string(rune('0'+i)),
			UserID:    "user-1",
			Symbol:    "BTC-USDT",
			Side:      types.OrderSideSell,
			Type:      types.OrderTypeLimit,
			Price:     decimal.NewFromInt(int64(50100 + i*100)),
			Quantity:  decimal.NewFromInt(1),
			FilledQty: decimal.Zero,
			Status:    types.OrderStatusNew,
		}
		ob.AddOrder(sellOrder)
	}

	snapshot := ob.Snapshot(5)

	if len(snapshot.Bids) != 5 {
		t.Errorf("expected 5 bid levels, got %d", len(snapshot.Bids))
	}
	if len(snapshot.Asks) != 5 {
		t.Errorf("expected 5 ask levels, got %d", len(snapshot.Asks))
	}

	// 验证买盘是降序
	for i := 0; i < len(snapshot.Bids)-1; i++ {
		if snapshot.Bids[i].Price.LessThan(snapshot.Bids[i+1].Price) {
			t.Error("bids should be in descending order")
		}
	}

	// 验证卖盘是升序
	for i := 0; i < len(snapshot.Asks)-1; i++ {
		if snapshot.Asks[i].Price.GreaterThan(snapshot.Asks[i+1].Price) {
			t.Error("asks should be in ascending order")
		}
	}
}

func TestOrderBook_UserOrders(t *testing.T) {
	ob := NewOrderBook("BTC-USDT")

	// 添加多个用户的订单
	for i := 0; i < 5; i++ {
		order := &types.Order{
			OrderID:   "user1-order-" + string(rune('0'+i)),
			UserID:    "user-1",
			Symbol:    "BTC-USDT",
			Side:      types.OrderSideBuy,
			Type:      types.OrderTypeLimit,
			Price:     decimal.NewFromInt(int64(50000 - i*100)),
			Quantity:  decimal.NewFromInt(1),
			FilledQty: decimal.Zero,
			Status:    types.OrderStatusNew,
		}
		ob.AddOrder(order)
	}

	for i := 0; i < 3; i++ {
		order := &types.Order{
			OrderID:   "user2-order-" + string(rune('0'+i)),
			UserID:    "user-2",
			Symbol:    "BTC-USDT",
			Side:      types.OrderSideSell,
			Type:      types.OrderTypeLimit,
			Price:     decimal.NewFromInt(int64(50100 + i*100)),
			Quantity:  decimal.NewFromInt(1),
			FilledQty: decimal.Zero,
			Status:    types.OrderStatusNew,
		}
		ob.AddOrder(order)
	}

	// 获取用户订单
	user1Orders := ob.GetUserOrders("user-1")
	if len(user1Orders) != 5 {
		t.Errorf("expected 5 orders for user-1, got %d", len(user1Orders))
	}

	user2Orders := ob.GetUserOrders("user-2")
	if len(user2Orders) != 3 {
		t.Errorf("expected 3 orders for user-2, got %d", len(user2Orders))
	}
}

func BenchmarkOrderBook_AddOrder(b *testing.B) {
	ob := NewOrderBook("BTC-USDT")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		order := &types.Order{
			OrderID:   "order-" + string(rune(i)),
			UserID:    "user-1",
			Symbol:    "BTC-USDT",
			Side:      types.OrderSideBuy,
			Type:      types.OrderTypeLimit,
			Price:     decimal.NewFromInt(int64(50000 + i%1000)),
			Quantity:  decimal.NewFromInt(1),
			FilledQty: decimal.Zero,
			Status:    types.OrderStatusNew,
		}
		ob.AddOrder(order)
	}
}

func BenchmarkOrderBook_Snapshot(b *testing.B) {
	ob := NewOrderBook("BTC-USDT")

	// 预先添加订单
	for i := 0; i < 10000; i++ {
		order := &types.Order{
			OrderID:   "order-" + string(rune(i)),
			UserID:    "user-1",
			Symbol:    "BTC-USDT",
			Side:      types.OrderSideBuy,
			Type:      types.OrderTypeLimit,
			Price:     decimal.NewFromInt(int64(50000 + i%1000)),
			Quantity:  decimal.NewFromInt(1),
			FilledQty: decimal.Zero,
			Status:    types.OrderStatusNew,
		}
		ob.AddOrder(order)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ob.Snapshot(20)
	}
}

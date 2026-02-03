package orderbook

import "errors"

var (
	ErrOrderNotFound           = errors.New("order not found")
	ErrAuctionNotInCallPhase   = errors.New("auction is not in call phase")
	ErrOnlyLimitOrderInAuction = errors.New("only limit orders are accepted in auction")
)

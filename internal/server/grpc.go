// Package server adapts the LedgerService gRPC contract to the application
// service, mapping domain errors onto gRPC status codes.
package server

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ledgerv1 "github.com/nvsces/ledger/gen/ledger/v1"
	"github.com/nvsces/ledger/internal/domain"
	"github.com/nvsces/ledger/internal/service"
)

// LedgerServer implements the generated LedgerServiceServer.
type LedgerServer struct {
	ledgerv1.UnimplementedLedgerServiceServer
	svc *service.Ledger
}

// NewLedgerServer wires the gRPC adapter over the application service.
func NewLedgerServer(svc *service.Ledger) *LedgerServer {
	return &LedgerServer{svc: svc}
}

func (s *LedgerServer) CreateAccount(ctx context.Context, req *ledgerv1.CreateAccountRequest) (*ledgerv1.Account, error) {
	acc, err := s.svc.CreateAccount(ctx, req.GetUserId(), req.GetCurrencyId())
	if err != nil {
		return nil, toStatus(err)
	}
	return accountToProto(acc), nil
}

func (s *LedgerServer) GetAccountByMemo(ctx context.Context, req *ledgerv1.GetAccountByMemoRequest) (*ledgerv1.Account, error) {
	acc, err := s.svc.AccountByMemo(ctx, req.GetMemo())
	if err != nil {
		return nil, toStatus(err)
	}
	return accountToProto(acc), nil
}

func (s *LedgerServer) Freeze(ctx context.Context, req *ledgerv1.FreezeRequest) (*ledgerv1.OpResponse, error) {
	applied, err := s.svc.Freeze(ctx, req.GetOrderId(), req.GetWalletId(), req.GetCurrencyId(), req.GetAmount())
	if err != nil {
		return nil, toStatus(err)
	}
	return &ledgerv1.OpResponse{OrderId: req.GetOrderId(), Applied: applied}, nil
}

func (s *LedgerServer) Capture(ctx context.Context, req *ledgerv1.CaptureRequest) (*ledgerv1.OpResponse, error) {
	applied, err := s.svc.Capture(ctx, req.GetOrderId(), req.GetWalletId(), req.GetReceivingWalletId(),
		req.GetCurrencyId(), req.GetNet(), req.GetFee())
	if err != nil {
		return nil, toStatus(err)
	}
	return &ledgerv1.OpResponse{OrderId: req.GetOrderId(), Applied: applied}, nil
}

func (s *LedgerServer) Release(ctx context.Context, req *ledgerv1.ReleaseRequest) (*ledgerv1.OpResponse, error) {
	applied, err := s.svc.Release(ctx, req.GetOrderId(), req.GetWalletId(), req.GetCurrencyId())
	if err != nil {
		return nil, toStatus(err)
	}
	return &ledgerv1.OpResponse{OrderId: req.GetOrderId(), Applied: applied}, nil
}

func (s *LedgerServer) Deposit(ctx context.Context, req *ledgerv1.DepositRequest) (*ledgerv1.OpResponse, error) {
	applied, err := s.svc.Deposit(ctx, req.GetRef(), req.GetWalletId(), req.GetCurrencyId(), req.GetAmount())
	if err != nil {
		return nil, toStatus(err)
	}
	return &ledgerv1.OpResponse{OrderId: req.GetRef(), Applied: applied}, nil
}

func (s *LedgerServer) GetBalance(ctx context.Context, req *ledgerv1.GetBalanceRequest) (*ledgerv1.BalanceResponse, error) {
	b, err := s.svc.Balance(ctx, req.GetWalletId(), req.GetCurrencyId())
	if err != nil {
		return nil, toStatus(err)
	}
	return &ledgerv1.BalanceResponse{
		WalletId:   b.WalletID,
		CurrencyId: b.CurrencyID,
		Available:  b.Available,
		Held:       b.Held,
	}, nil
}

func (s *LedgerServer) ListEntries(ctx context.Context, req *ledgerv1.ListEntriesRequest) (*ledgerv1.ListEntriesResponse, error) {
	entries, err := s.svc.Entries(ctx, req.GetOrderId())
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*ledgerv1.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, &ledgerv1.Entry{
			Id:         e.ID,
			OrderId:    e.OrderID,
			Op:         string(e.Op),
			WalletId:   e.WalletID,
			Bucket:     string(e.Bucket),
			CurrencyId: e.CurrencyID,
			Amount:     e.Amount,
			CreatedAt:  e.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return &ledgerv1.ListEntriesResponse{Entries: out}, nil
}

func (s *LedgerServer) ListCurrencies(ctx context.Context, _ *ledgerv1.ListCurrenciesRequest) (*ledgerv1.ListCurrenciesResponse, error) {
	currencies, err := s.svc.ListCurrencies(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*ledgerv1.Currency, 0, len(currencies))
	for _, c := range currencies {
		out = append(out, &ledgerv1.Currency{
			Id:       c.ID,
			Code:     c.Code,
			Name:     c.Name,
			Symbol:   c.Symbol,
			Decimals: c.Decimals,
			IsReal:   c.IsReal,
		})
	}
	return &ledgerv1.ListCurrenciesResponse{Currencies: out}, nil
}

func accountToProto(a *domain.Account) *ledgerv1.Account {
	return &ledgerv1.Account{
		WalletId:   a.WalletID,
		UserId:     a.UserID,
		TonAddress: a.TONAddress,
		Memo:       a.Memo,
		CurrencyId: a.CurrencyID,
	}
}

// toStatus maps domain errors to gRPC codes.
func toStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrInsufficient):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, domain.ErrInvalidAmount), errors.Is(err, domain.ErrNonPositive):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrWalletNotFound), errors.Is(err, domain.ErrAccountNotFound),
		errors.Is(err, domain.ErrCurrencyNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

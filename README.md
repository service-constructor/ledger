# Ledger

The **settlement primitive** behind the [Service Constructor](../constructor)
payment saga (white paper §10). It is a standalone gRPC service, backed by a
**double-entry** PostgreSQL journal. It owns both **client accounts** (each with
a wallet balance and on-chain deposit routing) and the saga settlement
operations the orchestrator calls as a black box:

| RPC                | Effect                                                              |
|--------------------|--------------------------------------------------------------------|
| `CreateAccount`    | Provision a user's wallet + TON deposit address & unique memo tag.  |
| `GetAccountByMemo` | Resolve the account an on-chain deposit belongs to, by memo.        |
| `Deposit`          | Credit external funds (on-chain transfer) into `AVAILABLE`.         |
| `Freeze`           | `user.AVAILABLE → user.HELD` — reserve funds before execute.        |
| `Capture`          | `user.HELD → recv.AVAILABLE (net)` + `→ platform.AVAILABLE (fee)`.   |
| `Release`          | `user.HELD → user.AVAILABLE` — compensation.                        |
| `GetBalance`       | Read a wallet's `available`/`held` for a currency.                  |
| `ListEntries`      | Read an order's append-only journal (audit).                        |

## Accounts & deposits

The ledger is also the **accounts service**: `CreateAccount` provisions a wallet
for a user and assigns the on-chain deposit routing. In the demo every account
shares one TON address (`TON_ADDRESS`) but gets a **unique memo tag**, so an
incoming transfer is matched to an account by memo. `Deposit` is the
external-funding entry point — the one operation whose entries do **not** sum to
zero (money enters the system from outside); it is idempotent on a caller ref
(the tx hash). The [auth](../auth) service calls `CreateAccount` at registration
and drives deposits by memo.

## Model

A wallet's money lives in two **buckets** per currency: `AVAILABLE` (spendable)
and `HELD` (reserved for an in-flight order). Every operation posts a **balanced
set of journal entries** whose signed amounts sum to exactly zero — money is only
ever moved, never created or destroyed. Balances are a materialized running total
(`wallet_balances`) updated in the **same transaction** as the journal rows, so a
balance read never disagrees with the journal.

Key properties:

- **Idempotent by `(order_id, op)`.** A replayed Freeze/Capture/Release inserts
  nothing and returns `applied=false`. This is what lets the saga's outbox
  dispatcher retry safely (Service Constructor white paper §11).
- **No negative balances.** A `CHECK (available >= 0 AND held >= 0)` on
  `wallet_balances` rejects any over-spend; the repository maps it to a
  `FailedPrecondition` / `ErrInsufficient`.
- **Exact money math.** Amounts are decimal strings on the wire, parsed with
  `shopspring/decimal` and stored as `NUMERIC(38,18)` — never floats.
- **Append-only audit.** The `entries` journal is only ever inserted into.

## Wiring into Service Constructor

Service Constructor defines `saga.Ledger` as a port and today ships an in-memory
mock. To use this service, implement that port with a gRPC client adapter (one
`LedgerServiceClient` call per method) and inject it in `cmd/server/main.go` in
place of `saga.NewMockLedger()`.

## Run

```bash
# 1. A Postgres with a `ledger` database:
createdb ledger    # or: docker exec <pg> createdb -U sc ledger

# 2. Start the service (migrations apply at startup):
DATABASE_URL="postgres://sc:sc@localhost:5432/ledger?sslmode=disable" \
GRPC_ADDR=":9110" PLATFORM_WALLET="wlt_platform" make run
```

Config (env): `DATABASE_URL`, `GRPC_ADDR` (default `:9100`), `PLATFORM_WALLET`
(default `wlt_platform`).

## Test

```bash
make test    # integration tests; skip cleanly if no DB is reachable
```

Tests cover the freeze→capture happy path with fee split, freeze→release,
idempotent replay (no double-debit), insufficient-funds rejection with clean
rollback, and the double-entry invariant (journal sums to zero).

## Layout

```
proto/ledger/v1/ledger.proto     gRPC contract
gen/                             generated stubs (buf generate)
internal/domain/                 money, buckets, entry/balance types
internal/repository/postgres/    double-entry postings + balances (the core)
internal/service/                validation + application logic
internal/server/                 gRPC adapter
cmd/server/                      entrypoint
migrations/                      embedded SQL (applied at startup)
```

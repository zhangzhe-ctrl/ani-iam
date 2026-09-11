// Package data contains infrastructure adapters.
package data

import "github.com/jackc/pgx/v5/pgxpool"

// Data owns long-lived infrastructure clients shared by repository adapters.
// It contains no transaction-scoped state.
type Data struct {
	pool   *pgxpool.Pool
	outbox *OutboxProtector
}

func NewData(pool *pgxpool.Pool, protectors ...*OutboxProtector) *Data {
	var p *OutboxProtector
	if len(protectors) == 1 {
		p = protectors[0]
	}
	return &Data{pool: pool, outbox: p}
}

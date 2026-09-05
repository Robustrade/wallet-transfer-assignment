package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// LedgerEntry holds the schema definition for the LedgerEntry entity.
type LedgerEntry struct {
	ent.Schema
}

// Fields of the LedgerEntry.
func (LedgerEntry) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),

		field.UUID("transfer_id", uuid.UUID{}).
			Immutable(),

		field.UUID("wallet_id", uuid.UUID{}).
			Immutable(),

		field.Enum("entry_type").
			Values(
				"DEBIT",
				"CREDIT",
			).
			Immutable(),

		field.Int64("amount").
			Positive().
			Immutable(),

		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Edges of the LedgerEntry.
func (LedgerEntry) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("wallet", Wallet.Type).
			Ref("ledger_entries").
			Field("wallet_id").
			Unique().
			Required().
			Immutable(),

		edge.From("transfer", Transfer.Type).
			Ref("ledger_entries").
			Field("transfer_id").
			Unique().
			Required().
			Immutable(),
	}
}

func (LedgerEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields(
			"transfer_id",
			"wallet_id",
			"entry_type",
		).Unique(),

		index.Fields("wallet_id"),

		index.Fields("transfer_id"),
	}
}

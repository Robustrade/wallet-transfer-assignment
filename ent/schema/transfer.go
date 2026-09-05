package schema

import (
	"wallet-transfer-assignment/ent/schema/mixin"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Transfer holds the schema definition for the Transfer entity.
type Transfer struct {
	ent.Schema
}

func (Transfer) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.TimeMixin{},
	}
}

// Fields of the Transfer.
func (Transfer) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),

		field.String("idempotency_key").
			NotEmpty().
			Immutable(),

		field.UUID("from_wallet_id", uuid.UUID{}).
			Immutable(),

		field.UUID("to_wallet_id", uuid.UUID{}).
			Immutable(),

		field.Int64("amount").
			Positive().
			Immutable(),

		field.Enum("status").
			Values(
				"PENDING",
				"PROCESSED",
				"FAILED",
			).
			Default("PENDING"),

		field.Enum("failure_reason").
			Values(
				"NONE",
				"INSUFFICIENT_FUNDS",
				"WALLET_NOT_FOUND",
			).
			Default("NONE"),
	}
}

// Edges of the Transfer.
func (Transfer) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("from_wallet", Wallet.Type).
			Ref("outgoing_transfers").
			Field("from_wallet_id").
			Unique().
			Required().
			Immutable(),

		edge.From("to_wallet", Wallet.Type).
			Ref("incoming_transfers").
			Field("to_wallet_id").
			Unique().
			Required().
			Immutable(),

		edge.To("ledger_entries", LedgerEntry.Type),
	}
}

func (Transfer) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("idempotency_key").
			Unique(),

		index.Fields("from_wallet_id"),

		index.Fields("to_wallet_id"),
	}
}

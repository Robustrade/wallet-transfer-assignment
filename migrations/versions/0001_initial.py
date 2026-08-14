"""initial schema

Revision ID: 0001
Revises:
Create Date: 2026-08-14
"""
import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "0001"
down_revision = None
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "wallets",
        sa.Column("id", sa.String(), primary_key=True),
        sa.Column("balance", sa.BigInteger(), nullable=False, server_default="0"),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint("balance >= 0", name="balance_non_negative"),
    )

    op.create_table(
        "transfers",
        sa.Column("id", sa.String(), primary_key=True),
        sa.Column("from_wallet", sa.String(), sa.ForeignKey("wallets.id"), nullable=False),
        sa.Column("to_wallet", sa.String(), sa.ForeignKey("wallets.id"), nullable=False),
        sa.Column("amount", sa.BigInteger(), nullable=False),
        sa.Column("status", sa.String(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint("amount > 0", name="transfer_amount_positive"),
        sa.CheckConstraint("status IN ('PENDING','PROCESSED','FAILED')", name="transfer_status_valid"),
        sa.CheckConstraint("from_wallet <> to_wallet", name="no_self_transfer"),
    )

    op.create_table(
        "ledger_entries",
        sa.Column("id", sa.BigInteger(), primary_key=True, autoincrement=True),
        sa.Column("transfer_id", sa.String(), sa.ForeignKey("transfers.id"), nullable=False),
        sa.Column("wallet_id", sa.String(), sa.ForeignKey("wallets.id"), nullable=False),
        sa.Column("type", sa.String(), nullable=False),
        sa.Column("amount", sa.BigInteger(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint("type IN ('DEBIT','CREDIT')", name="ledger_type_valid"),
        sa.CheckConstraint("amount > 0", name="ledger_amount_positive"),
        sa.UniqueConstraint("transfer_id", "type", name="one_entry_per_side"),
    )
    op.create_index("idx_ledger_wallet", "ledger_entries", ["wallet_id"])

    op.create_table(
        "idempotency_records",
        sa.Column("idempotency_key", sa.String(), primary_key=True),
        sa.Column("request_hash", sa.String(), nullable=False),
        sa.Column("transfer_id", sa.String(), sa.ForeignKey("transfers.id"), nullable=True),
        sa.Column("response_body", postgresql.JSONB(), nullable=True),
        sa.Column("status", sa.String(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint("status IN ('IN_PROGRESS','COMPLETED')", name="idempotency_status_valid"),
    )


def downgrade() -> None:
    op.drop_table("idempotency_records")
    op.drop_index("idx_ledger_wallet", table_name="ledger_entries")
    op.drop_table("ledger_entries")
    op.drop_table("transfers")
    op.drop_table("wallets")

package com.example.wallet.handler;

import com.example.wallet.domain.Wallet;
import java.time.Instant;

public record WalletResponse(String id, long balance, Instant updatedAt) {

    static WalletResponse from(Wallet w) {
        return new WalletResponse(w.id(), w.balance(), w.updatedAt());
    }
}

package com.walletTransfer.walletTransfer.util;

import java.util.List;
import java.util.UUID;
import java.util.stream.Stream;

public final  class WalletLockHelper {
    private WalletLockHelper() {
    }

    public static List<UUID> sortWalletIds(
            UUID walletOne,
            UUID walletTwo) {

        return Stream.of(walletOne, walletTwo)
                .sorted()
                .toList();
    }
}


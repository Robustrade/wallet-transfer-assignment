package com.kullu.wallet.support;

import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicInteger;

import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ContextConfiguration;

import com.kullu.wallet.entity.WalletEntity;
import com.kullu.wallet.repository.WalletRepository;

@SpringBootTest
@ContextConfiguration(initializers = PostgresContainerInitializer.class)
public abstract class AbstractPostgresIntegrationTest {

    private static final AtomicInteger WALLET_SEQUENCE = new AtomicInteger();

    /**
     * Records each wallet's seeded initial balance so assertions can pin the
     * full double-entry invariant {@code stored == initial + Σledger} rather
     * than the weaker {@code stored - Σledger >= 0} smoke check.
     */
    private static final Map<String, Long> INITIAL_BALANCES = new ConcurrentHashMap<>();

    protected static String seedWallet(WalletRepository wallets, long balance) {
        WalletEntity w = new WalletEntity("wallet-" + WALLET_SEQUENCE.incrementAndGet(), balance);
        wallets.saveAndFlush(w);
        INITIAL_BALANCES.put(w.getId(), balance);
        return w.getId();
    }

    /**
     * Returns the initial balance captured at seed time.
     *
     * @throws IllegalStateException if {@code walletId} was not seeded via
     *     {@link #seedWallet(WalletRepository, long)}
     */
    protected static long initialBalanceOf(String walletId) {
        Long initial = INITIAL_BALANCES.get(walletId);
        if (initial == null) {
            throw new IllegalStateException(
                "wallet " + walletId + " was not seeded via seedWallet(); initial balance unknown");
        }
        return initial;
    }
}

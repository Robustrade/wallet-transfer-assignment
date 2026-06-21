package com.mycompany.wallet_transaction.repository;

import com.mycompany.wallet_transaction.entities.Wallet;
import jakarta.persistence.LockModeType;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

import java.util.List;

public interface WalletRepository extends JpaRepository<Wallet, Long> {

    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query(value = """
           select w from Wallet w where w.walletId in :walletIds order by w.walletId
           """)
    List<Wallet> findAllByWalletIdInForUpdate(@Param("walletIds") List<Long> walletIds);
}

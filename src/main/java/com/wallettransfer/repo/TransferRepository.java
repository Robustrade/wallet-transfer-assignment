package com.wallettransfer.repo;

import com.wallettransfer.entity.Transfer;
import java.util.Optional;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

@Repository
public interface TransferRepository extends JpaRepository<Transfer, String> {

  Optional<Transfer> findByIdempotencyKey(String idempotencyKey);
}

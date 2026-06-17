package com.wallet.transfer.repository;

import com.wallet.transfer.model.UserEntry;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.Optional;

@Repository
public interface UserRepository extends JpaRepository<UserEntry, Long> {

    Optional<UserEntry> findByUsername(String username);

    Optional<UserEntry> findByEmail(String email);

    Optional<UserEntry> findByPhoneno(String phoneno);
}

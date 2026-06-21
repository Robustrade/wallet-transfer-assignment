package com.rajat.wallet.service;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.rajat.wallet.domain.entities.IdempotencyRecord;
import com.rajat.wallet.domain.enums.IdempotencyStatus;
import com.rajat.wallet.dto.CreateTransferRequest;
import com.rajat.wallet.dto.TransferResponse;
import com.rajat.wallet.exception.DataIntegrityViolations;
import com.rajat.wallet.exception.IdempotencyConflictException;
import com.rajat.wallet.repository.IdempotencyRecordRepository;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;
import java.util.Optional;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;

/**
 * Idempotency-aware orchestrator. Deliberately <b>not</b> {@code @Transactional}: the atomic unit
 * of work lives in {@link TransferProcessor}, and keeping this layer outside any transaction is
 * what lets it catch a concurrent duplicate's failure and replay the winner in a fresh read.
 *
 * <ul>
 *   <li><b>Fast path</b> — a key we've already seen (the common sequential-retry case) is replayed
 *       without touching the wallets.
 *   <li><b>First occurrence</b> — delegated to {@link TransferProcessor#process} (one transaction).
 *   <li><b>Concurrent duplicate</b> — the loser blocks on the unique index until the winner
 *       commits, fails with a {@link DataIntegrityViolationException}, and is then replayed
 *       directly.
 * </ul>
 */
@Service
@Slf4j
@RequiredArgsConstructor
public class TransferServiceImpl implements TransferService {

  private final TransferProcessor transferProcessor;
  private final IdempotencyRecordRepository idempotencyRepository;
  private final ObjectMapper objectMapper;

  @Override
  public TransferResponse createTransfer(CreateTransferRequest request) {
    String requestHash = requestHash(request);

    Optional<IdempotencyRecord> existing =
        idempotencyRepository.findByIdempotencyKey(request.idempotencyKey());
    if (existing.isPresent()) {
      return replayOrConflict(existing.get(), requestHash);
    }

    try {
      return transferProcessor.process(request, requestHash);
    } catch (DataIntegrityViolationException ex) {
      // Only the idempotency-key unique violation is our replayable race. Any other integrity
      // violation (FK, CHECK, NOT NULL) is a genuine failure — rethrow it so it surfaces as a 500
      // rather than being misread as a duplicate and turned into a 409.
      if (!DataIntegrityViolations.isIdempotencyKeyViolation(ex)) {
        throw ex;
      }
      // A concurrent first request won the unique-key race and has now committed. Replay it.
      log.info("Lost idempotency-key race for {}; replaying winner", request.idempotencyKey());
      IdempotencyRecord winner =
          idempotencyRepository
              .findByIdempotencyKey(request.idempotencyKey())
              .orElseThrow(() -> ex);
      return replayOrConflict(winner, requestHash);
    }
  }

  private TransferResponse replayOrConflict(IdempotencyRecord record, String requestHash) {
    // Defensive guard. With the current strategy (reservation + completion in one transaction) a
    // committed record is always already COMPLETED, so under READ COMMITTED no other request can
    // observe a committed IN_PROGRESS — concurrent duplicates block on the unique index and then
    // replay the winner. This branch only becomes reachable if the reservation is ever moved to its
    // own committed transaction.
    if (record.getStatus() == IdempotencyStatus.IN_PROGRESS) {
      throw new IdempotencyConflictException(
          "A request with idempotency key '"
              + record.getIdempotencyKey()
              + "' is still in progress; retry shortly");
    }
    if (!record.getRequestHash().equals(requestHash)) {
      throw new IdempotencyConflictException(
          "Idempotency key '"
              + record.getIdempotencyKey()
              + "' was already used for a different request");
    }
    log.info("Replaying cached response for idempotency key {}", record.getIdempotencyKey());
    return deserialize(record.getResponseBody());
  }

  /** Stable fingerprint of the meaningful request fields; ties a key to one logical request. */
  private String requestHash(CreateTransferRequest request) {
    String canonical =
        request.fromWalletId()
            + "|"
            + request.toWalletId()
            + "|"
            + request.amount().stripTrailingZeros().toPlainString();
    try {
      byte[] hash =
          MessageDigest.getInstance("SHA-256").digest(canonical.getBytes(StandardCharsets.UTF_8));
      return HexFormat.of().formatHex(hash);
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException("SHA-256 unavailable", e);
    }
  }

  private TransferResponse deserialize(String body) {
    try {
      return objectMapper.readValue(body, TransferResponse.class);
    } catch (JsonProcessingException e) {
      throw new IllegalStateException("Failed to deserialize cached response", e);
    }
  }
}

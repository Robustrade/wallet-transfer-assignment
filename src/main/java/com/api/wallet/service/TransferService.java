package com.api.wallet.service;

import com.api.wallet.domain.IdempotencyRecord;
import com.api.wallet.dto.CreateTransferRequest;
import com.api.wallet.dto.TransferResponse;
import com.api.wallet.exception.WalletNotFoundException;
import com.api.wallet.repository.IdempotencyRecordRepository;
import com.api.wallet.repository.TransferRepository;
import com.api.wallet.repository.WalletRepository;
import java.util.List;
import java.util.Optional;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import tools.jackson.databind.json.JsonMapper;

@Service
public class TransferService {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final JsonMapper jsonMapper;
    private final TransferAttemptExecutor transferAttemptExecutor;

    public TransferService(
            WalletRepository walletRepository,
            TransferRepository transferRepository,
            IdempotencyRecordRepository idempotencyRecordRepository,
            JsonMapper jsonMapper,
            TransferAttemptExecutor transferAttemptExecutor) {
        this.walletRepository = walletRepository;
        this.transferRepository = transferRepository;
        this.idempotencyRecordRepository = idempotencyRecordRepository;
        this.jsonMapper = jsonMapper;
        this.transferAttemptExecutor = transferAttemptExecutor;
    }

    public TransferResponse createTransfer(CreateTransferRequest request) {
        if (request.fromWalletId().equals(request.toWalletId())) {
            throw new IllegalArgumentException("fromWalletId and toWalletId must differ");
        }

        Optional<IdempotencyRecord> existing =
                idempotencyRecordRepository.findById(request.idempotencyKey());
        if (existing.isPresent()) {
            return replay(existing.get());
        }

        try {
            return transferAttemptExecutor.attemptTransfer(request);
        } catch (DataIntegrityViolationException raceLost) {
            IdempotencyRecord winner =
                    idempotencyRecordRepository
                            .findById(request.idempotencyKey())
                            .orElseThrow(() -> raceLost);
            return replay(winner);
        }
    }

    @Transactional(readOnly = true)
    public List<TransferResponse> getHistory(String walletId) {
        if (!walletRepository.existsById(walletId)) {
            throw new WalletNotFoundException(walletId);
        }
        return transferRepository.findByFromWalletIdOrToWalletIdOrderByCreatedAtDesc(walletId, walletId).stream()
                .map(TransferResponse::from)
                .toList();
    }

    private TransferResponse replay(IdempotencyRecord existing) {
        return jsonMapper.readValue(existing.getResponseBody(), TransferResponse.class);
    }
}

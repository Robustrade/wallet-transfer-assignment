package com.example.wallettransfer.service;

import com.example.wallettransfer.domain.Transfer;
import com.example.wallettransfer.domain.Wallet;
import com.example.wallettransfer.dto.TransferHistoryItem;
import com.example.wallettransfer.dto.TransferHistoryResponse;
import com.example.wallettransfer.dto.WalletResponse;
import com.example.wallettransfer.exception.InvalidTransferException;
import com.example.wallettransfer.exception.WalletNotFoundException;
import com.example.wallettransfer.repository.TransferRepository;
import com.example.wallettransfer.repository.WalletRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.data.domain.PageRequest;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.Base64;
import java.util.List;
import java.util.UUID;

@Service
@RequiredArgsConstructor
@Transactional(readOnly = true)
public class WalletQueryServiceImpl implements WalletQueryService {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;

    @Override
    public WalletResponse getWallet(String walletId) {
        Wallet wallet = walletRepository.findById(walletId)
                .orElseThrow(() -> new WalletNotFoundException(walletId));
        return new WalletResponse(wallet.getId(), wallet.getBalance());
    }

    @Override
    public TransferHistoryResponse getTransferHistory(String walletId, int limit, String cursor) {
        if (!walletRepository.existsById(walletId)) {
            throw new WalletNotFoundException(walletId);
        }

        HistoryCursor historyCursor = cursor == null ? null : HistoryCursor.decode(cursor);
        List<Transfer> results = historyCursor == null
                ? transferRepository.findFirstHistory(walletId, PageRequest.of(0, limit + 1))
                : transferRepository.findHistoryAfter(walletId, historyCursor.createdAt(), historyCursor.transferId(),
                        PageRequest.of(0, limit + 1));

        boolean hasNextPage = results.size() > limit;
        List<Transfer> page = hasNextPage ? results.subList(0, limit) : results;
        String nextCursor = hasNextPage ? HistoryCursor.from(page.get(page.size() - 1)).encode() : null;
        return new TransferHistoryResponse(page.stream().map(this::toItem).toList(), nextCursor);
    }

    private TransferHistoryItem toItem(Transfer transfer) {
        return new TransferHistoryItem(transfer.getId(), transfer.getFromWalletId(), transfer.getToWalletId(),
                transfer.getAmount(), transfer.getStatus(), transfer.getFailureReason(), transfer.getCreatedAt(),
                transfer.getCompletedAt());
    }

    private record HistoryCursor(Instant createdAt, UUID transferId) {
        private static HistoryCursor from(Transfer transfer) {
            return new HistoryCursor(transfer.getCreatedAt(), transfer.getId());
        }

        private String encode() {
            return Base64.getUrlEncoder().withoutPadding().encodeToString(
                    (createdAt + "|" + transferId).getBytes(StandardCharsets.UTF_8));
        }

        private static HistoryCursor decode(String value) {
            try {
                String decoded = new String(Base64.getUrlDecoder().decode(value), StandardCharsets.UTF_8);
                String[] parts = decoded.split("\\|", -1);
                if (parts.length != 2) {
                    throw new IllegalArgumentException();
                }
                return new HistoryCursor(Instant.parse(parts[0]), UUID.fromString(parts[1]));
            } catch (IllegalArgumentException exception) {
                throw new InvalidTransferException("Invalid transfer history cursor");
            }
        }
    }
}

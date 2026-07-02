package com.wallettransfer.service.walletservice;

import com.wallettransfer.builder.ResponseBuilderFactory;
import com.wallettransfer.constants.ApplicationConstants;
import com.wallettransfer.constants.ResponseConstant;
import com.wallettransfer.dto.requestdto.TransferHistoryRequestDTO;
import com.wallettransfer.dto.requestdto.WalletBalanceRequestDTO;
import com.wallettransfer.dto.responsedto.ResponseDTO;
import com.wallettransfer.dto.responsedto.TransferResponseDTO;
import com.wallettransfer.dto.responsedto.WalletBalanceResponseDTO;
import com.wallettransfer.entity.LedgerEntry;
import com.wallettransfer.entity.Transfer;
import com.wallettransfer.entity.Wallet;
import com.wallettransfer.exception.ResponseException;
import com.wallettransfer.repo.LedgerEntryRepository;
import com.wallettransfer.repo.TransferRepository;
import com.wallettransfer.repo.WalletRepository;
import com.wallettransfer.service.loggerservice.LogService;
import com.wallettransfer.utility.ServiceResponseSupport;
import com.wallettransfer.utility.TransferMapper;
import jakarta.servlet.http.HttpServletRequest;
import java.util.Comparator;
import java.util.List;
import java.util.Map;
import java.util.function.Function;
import java.util.stream.Collectors;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class WalletService {

  private final String className = this.getClass().getSimpleName();
  private final LogService logService;
  private final HttpServletRequest request;
  private final ServiceResponseSupport serviceResponseSupport;
  private final WalletRepository walletRepository;
  private final LedgerEntryRepository ledgerEntryRepository;
  private final TransferRepository transferRepository;

  public WalletService(
      LogService logService,
      HttpServletRequest request,
      ServiceResponseSupport serviceResponseSupport,
      WalletRepository walletRepository,
      LedgerEntryRepository ledgerEntryRepository,
      TransferRepository transferRepository) {
    this.logService = logService;
    this.request = request;
    this.serviceResponseSupport = serviceResponseSupport;
    this.walletRepository = walletRepository;
    this.ledgerEntryRepository = ledgerEntryRepository;
    this.transferRepository = transferRepository;
  }

  @Transactional(readOnly = true)
  public ResponseDTO getWalletBalance(WalletBalanceRequestDTO balanceRequest) {
    logService.printInfoLogs(request, className, "Get Wallet Balance");
    return serviceResponseSupport.execute(
        className,
        () -> {
          Wallet wallet =
              walletRepository
                  .findById(balanceRequest.getWalletId())
                  .orElseThrow(
                      () ->
                          new ResponseException(
                              "Wallet not found", ResponseConstant.WALLET_NOT_FOUND));

          WalletBalanceResponseDTO payload =
              WalletBalanceResponseDTO.builder()
                  .walletId(wallet.getWalletId())
                  .balance(wallet.getBalance())
                  .build();

          return ResponseBuilderFactory.getResponse(
              ApplicationConstants.SUCCESS,
              ResponseConstant.BALANCE_FETCH_SUCCESS,
              payload);
        });
  }

  @Transactional(readOnly = true)
  public ResponseDTO getTransferHistory(TransferHistoryRequestDTO historyRequest) {
    logService.printInfoLogs(request, className, "Get Transfer History");
    return serviceResponseSupport.execute(
        className,
        () -> {
          if (!walletRepository.existsById(historyRequest.getWalletId())) {
            throw new ResponseException("Wallet not found", ResponseConstant.WALLET_NOT_FOUND);
          }

          List<LedgerEntry> ledgerEntries =
              ledgerEntryRepository.findByWallet_WalletIdOrderByCreatedAtDesc(
                  historyRequest.getWalletId());

          List<String> transferIds =
              ledgerEntries.stream().map(entry -> entry.getTransfer().getId()).distinct().toList();

          Map<String, Transfer> transfersById =
              transferRepository.findAllById(transferIds).stream()
                  .collect(Collectors.toMap(Transfer::getId, Function.identity()));

          List<TransferResponseDTO> history =
              transferIds.stream()
                  .map(
                      transferId -> {
                        Transfer transfer = transfersById.get(transferId);
                        List<LedgerEntry> entriesForTransfer =
                            ledgerEntryRepository.findByTransfer_Id(transferId).stream()
                                .sorted(
                                    Comparator.comparing(entry -> entry.getEntryType().name()))
                                .toList();
                        return TransferMapper.toTransferResponse(transfer, entriesForTransfer);
                      })
                  .toList();

          return ResponseBuilderFactory.getResponse(
              ApplicationConstants.SUCCESS,
              ResponseConstant.HISTORY_FETCH_SUCCESS,
              history);
        });
  }
}

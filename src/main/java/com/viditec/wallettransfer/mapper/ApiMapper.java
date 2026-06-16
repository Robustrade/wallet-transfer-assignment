package com.viditec.wallettransfer.mapper;

import com.viditec.wallettransfer.api.request.CreateTransferRequest;
import com.viditec.wallettransfer.api.request.CreateWalletRequest;
import com.viditec.wallettransfer.api.response.TransferResponse;
import com.viditec.wallettransfer.api.response.WalletResponse;
import com.viditec.wallettransfer.dto.CreateTransferDto;
import com.viditec.wallettransfer.dto.CreateWalletDto;
import com.viditec.wallettransfer.dto.TransferDto;
import com.viditec.wallettransfer.dto.WalletDto;
import org.springframework.stereotype.Component;

@Component
public class ApiMapper {

    public CreateTransferDto toDto(CreateTransferRequest request) {
        return new CreateTransferDto(
                request.idempotencyKey(),
                request.fromWalletId(),
                request.toWalletId(),
                request.amount()
        );
    }

    public CreateWalletDto toDto(CreateWalletRequest request) {
        return new CreateWalletDto(
                request.walletId(),
                request.initialBalance()
        );
    }

    public TransferResponse toResponse(TransferDto dto) {
        return new TransferResponse(
                dto.transferId(),
                dto.idempotencyKey(),
                dto.fromWalletId(),
                dto.toWalletId(),
                dto.amount(),
                dto.state()
        );
    }

    public WalletResponse toResponse(WalletDto dto) {
        return new WalletResponse(
                dto.walletId(),
                dto.balance()
        );
    }
}

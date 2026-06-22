package com.walletTransfer.walletTransfer.mapper;

import com.walletTransfer.walletTransfer.dto.TransferResponse;
import com.walletTransfer.walletTransfer.entity.Transfer;
import org.mapstruct.Mapper;
import org.mapstruct.Mapping;

@Mapper(componentModel = "spring")
public interface TransferMapper {


    @Mapping(
            source = "id",
            target = "transferId"
    )
    TransferResponse toResponse(Transfer transfer);
}

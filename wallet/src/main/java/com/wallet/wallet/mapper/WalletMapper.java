package com.wallet.wallet.mapper;

import com.wallet.wallet.dto.WalletRequestDTO;
import com.wallet.wallet.dto.WalletResponseDTO;
import com.wallet.wallet.model.Wallet;
import org.mapstruct.Mapper;
import org.mapstruct.Mapping;

@Mapper(componentModel = "spring")
public interface WalletMapper {

    @Mapping(source = "amount", target = "totalBalance")
    @Mapping(source="amount",target = "availableBalance")
    Wallet toEntity(WalletRequestDTO walletRequestDTO);

    WalletResponseDTO toDto(Wallet wallet);

}

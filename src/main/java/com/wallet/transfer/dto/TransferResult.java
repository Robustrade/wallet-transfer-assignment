package com.wallet.transfer.dto;

import com.wallet.transfer.domain.Transfer;

public record TransferResult(Transfer transfer, boolean created) {
}

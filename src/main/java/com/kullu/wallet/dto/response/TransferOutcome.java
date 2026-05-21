package com.kullu.wallet.dto.response;

import lombok.AllArgsConstructor;
import lombok.Getter;

@Getter
@AllArgsConstructor
public final class TransferOutcome {

    private final boolean replayed;
    private final short status;
    private final String responseBody;
}

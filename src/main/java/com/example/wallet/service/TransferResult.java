package com.example.wallet.service;

import com.example.wallet.domain.Transfer;

/** Outcome of a create-transfer call. {@code replayed} is true when an earlier result was returned. */
public record TransferResult(Transfer transfer, boolean replayed) {}

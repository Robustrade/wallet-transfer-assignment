package com.robustrade.wallet.service;

import com.robustrade.wallet.entities.Wallet;

/** The two locked wallet rows. The fields are roles, not lock order. */
record LockedWallets(Wallet from, Wallet to) {}

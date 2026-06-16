package com.viditec.wallettransfer.repository;

import com.viditec.wallettransfer.domain.EntryType;

public interface LedgerOperations {

    void createEntry(String transferId, String walletId, EntryType entryType, long amount);
}

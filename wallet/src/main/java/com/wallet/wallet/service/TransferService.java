package com.wallet.wallet.service;

import com.wallet.wallet.dto.TransferRequestDTO;
import com.wallet.wallet.dto.TransferResponeDTO;
import org.springframework.stereotype.Service;

@Service
public class TransferService {

    /*
     * have to deal with the idempotency
     * Using the mapper get the Entity instance
     * create record in the transaction table, status as PROCESSING
     * acquire lock for the row and validate the balance
     * update the balance for the credit and debit wallets
     * ledger entry
     * update the transaction table records as SUCCESS
     * store the response in the idempotency table
     * return the response
     * */
    public TransferResponeDTO createTransfer(TransferRequestDTO transferRequestDTO){
        return null;
    }


}

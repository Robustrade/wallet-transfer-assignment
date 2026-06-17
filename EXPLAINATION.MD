Flow of Transactions
    check idempotency --> if exit then return the status of transactions
    if not
        create transactions
        lock wallets --> for both sender and receiver
            most important --> lock wallet in increasing id of wallet_id otherwise deadlock can occur.
        create transfer entry in db --> with pending status
        transfer money from user1 account
        insert ledger debit
        transfer money from user2(to user) wallet
        insert ledger credit
        update transfer with successfully
        create idempotency with successfull status
        commited
        otherwise rollbac if error
        


Database Choose;
    This is money transfer app so need to ensure ACID compilation. so choose postgres.
    wallet, transfer, ledger_entries and idempotency_records all are postgres.
    foreign key --
        wallets(wallet_id)  for transfers
        wallets(wallet_id)  for ledger_entries

    Add properly indexes on 
        transfers(from_wallet_id)
        transfers(to_wallet_id)

Transaction Safety
    when lock wallet --> lock id in increasing order



Test cases
    10 concurrent --> 100% successfull
    50 concurrent --> 100% successfull time ==> 1.0127165s
    100 concurrent --> 100% successfull time ==> 1.4835776s or 2.2954332s
    200 concurrent --> 100% successfull  time --> 2.3527446s sec
    400 concurrent (1 milli seccond gap)--> 100% successfull time ==> 2.9907291s
    1000 concurrent (1 milli seccond gap)--> 100% successfull time ==> 6.625749s 



    DEBIT == CREDIT  for all cases



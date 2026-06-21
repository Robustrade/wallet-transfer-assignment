package com.mycompany.wallet_transaction;

import com.mycompany.wallet_transaction.dto.PaymentRequestDto;
import com.mycompany.wallet_transaction.dto.PaymentResponseDto;
import com.mycompany.wallet_transaction.entities.Wallet;
import com.mycompany.wallet_transaction.entities.enums.TransferStatus;
import com.mycompany.wallet_transaction.repository.IdempotencyRecordRepository;
import com.mycompany.wallet_transaction.repository.LedgerEntryRepository;
import com.mycompany.wallet_transaction.repository.TransferRepository;
import com.mycompany.wallet_transaction.repository.WalletRepository;
import com.mycompany.wallet_transaction.service.PaymentService;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;

import java.math.BigDecimal;
import java.util.concurrent.*;
import java.util.stream.Stream;

import static org.junit.jupiter.api.Assertions.assertEquals;

@SpringBootTest
class WalletTransactionApplicationTests {

	@Autowired
	private PaymentService paymentService;

	@Autowired
	private WalletRepository walletRepository;

	@Autowired
	private TransferRepository transferRepository;

	@Autowired
	private LedgerEntryRepository ledgerEntryRepository;

	@Autowired
	private IdempotencyRecordRepository idempotencyRecordRepository;

	@BeforeEach
	void cleanUp() {
		ledgerEntryRepository.deleteAll();
		idempotencyRecordRepository.deleteAll();
		transferRepository.deleteAll();
		walletRepository.deleteAll();
	}

	@Test
	void successfulTransfer_shouldDebitSourceCreditDestinationAndCreateLedgerEntries() {
		Wallet source = new Wallet();
		source.setBalance(new BigDecimal("500.00"));
		source = walletRepository.save(source);

		Wallet destination = new Wallet();
		destination.setBalance(new BigDecimal("100.00"));
		destination = walletRepository.save(destination);

		PaymentRequestDto request = new PaymentRequestDto();
		request.setIdempotencyKey("idem-success-1");
		request.setFromWalletId(source.getWalletId());
		request.setToWalletId(destination.getWalletId());
		request.setAmount(new BigDecimal("150.00"));

		PaymentResponseDto response = paymentService.makeTransfer(request);

		assertEquals(TransferStatus.PROCESSED, response.getStatus());

		Wallet updatedSource = walletRepository.findById(source.getWalletId()).orElseThrow();
		Wallet updatedDestination = walletRepository.findById(destination.getWalletId()).orElseThrow();

		assertEquals(0, updatedSource.getBalance().compareTo(new BigDecimal("350.00")));
		assertEquals(0, updatedDestination.getBalance().compareTo(new BigDecimal("250.00")));

		assertEquals(1, transferRepository.count());
		assertEquals(2, ledgerEntryRepository.count());
	}

	@Test
	void insufficientBalance_shouldMarkTransferFailedAndNotCreateLedgerEntries() {
		Wallet source = new Wallet();
		source.setBalance(new BigDecimal("50.00"));
		source = walletRepository.save(source);

		Wallet destination = new Wallet();
		destination.setBalance(new BigDecimal("100.00"));
		destination = walletRepository.save(destination);

		PaymentRequestDto request = new PaymentRequestDto();
		request.setIdempotencyKey("idem-failed-1");
		request.setFromWalletId(source.getWalletId());
		request.setToWalletId(destination.getWalletId());
		request.setAmount(new BigDecimal("150.00"));

		PaymentResponseDto response = paymentService.makeTransfer(request);

		assertEquals(TransferStatus.FAILED, response.getStatus());

		Wallet updatedSource = walletRepository.findById(source.getWalletId()).orElseThrow();
		Wallet updatedDestination = walletRepository.findById(destination.getWalletId()).orElseThrow();

		assertEquals(0, updatedSource.getBalance().compareTo(new BigDecimal("50.00")));
		assertEquals(0, updatedDestination.getBalance().compareTo(new BigDecimal("100.00")));

		assertEquals(1, transferRepository.count());
		assertEquals(0, ledgerEntryRepository.count());
	}

	@Test
	void duplicateIdempotencyKey_shouldNotProcessTransferAgain() {
		Wallet source = new Wallet();
		source.setBalance(new BigDecimal("500.00"));
		source = walletRepository.save(source);

		Wallet destination = new Wallet();
		destination.setBalance(new BigDecimal("100.00"));
		destination = walletRepository.save(destination);

		PaymentRequestDto request = new PaymentRequestDto();
		request.setIdempotencyKey("idem-duplicate-1");
		request.setFromWalletId(source.getWalletId());
		request.setToWalletId(destination.getWalletId());
		request.setAmount(new BigDecimal("100.00"));

		PaymentResponseDto firstResponse = paymentService.makeTransfer(request);
		PaymentResponseDto secondResponse = paymentService.makeTransfer(request);

		assertEquals(TransferStatus.PROCESSED, firstResponse.getStatus());
		assertEquals(TransferStatus.PROCESSED, secondResponse.getStatus());

		Wallet updatedSource = walletRepository.findById(source.getWalletId()).orElseThrow();
		Wallet updatedDestination = walletRepository.findById(destination.getWalletId()).orElseThrow();

		assertEquals(0, updatedSource.getBalance().compareTo(new BigDecimal("400.00")));
		assertEquals(0, updatedDestination.getBalance().compareTo(new BigDecimal("200.00")));

		assertEquals(1, transferRepository.count());
		assertEquals(2, ledgerEntryRepository.count());
		assertEquals(1, idempotencyRecordRepository.count());
	}

	@Test
	void invalidWallet_shouldReturnFailedAndNotCreateLedgerEntries() {
		Wallet source = new Wallet();
		source.setBalance(new BigDecimal("500.00"));
		source = walletRepository.save(source);

		PaymentRequestDto request = new PaymentRequestDto();
		request.setIdempotencyKey("idem-invalid-wallet-1");
		request.setFromWalletId(source.getWalletId());
		request.setToWalletId(999999L);
		request.setAmount(new BigDecimal("100.00"));

		PaymentResponseDto response = paymentService.makeTransfer(request);

		assertEquals(TransferStatus.FAILED, response.getStatus());
		assertEquals(0, ledgerEntryRepository.count());
	}

	@Test
	void concurrentTransfersFromSameWallet_shouldAllowOnlyOneDebit() throws Exception {
		Wallet source = new Wallet();
		source.setBalance(new BigDecimal("100.00"));
		source = walletRepository.save(source);

		Wallet destination1 = new Wallet();
		destination1.setBalance(new BigDecimal("0.00"));
		destination1 = walletRepository.save(destination1);

		Wallet destination2 = new Wallet();
		destination2.setBalance(new BigDecimal("0.00"));
		destination2 = walletRepository.save(destination2);

		PaymentRequestDto request1 = new PaymentRequestDto();
		request1.setIdempotencyKey("idem-concurrent-1");
		request1.setFromWalletId(source.getWalletId());
		request1.setToWalletId(destination1.getWalletId());
		request1.setAmount(new BigDecimal("80.00"));

		PaymentRequestDto request2 = new PaymentRequestDto();
		request2.setIdempotencyKey("idem-concurrent-2");
		request2.setFromWalletId(source.getWalletId());
		request2.setToWalletId(destination2.getWalletId());
		request2.setAmount(new BigDecimal("80.00"));

		ExecutorService executorService = Executors.newFixedThreadPool(2);
		CountDownLatch startLatch = new CountDownLatch(1);

		Callable<PaymentResponseDto> task1 = () -> {
			startLatch.await();
			return paymentService.makeTransfer(request1);
		};

		Callable<PaymentResponseDto> task2 = () -> {
			startLatch.await();
			return paymentService.makeTransfer(request2);
		};

		Future<PaymentResponseDto> future1 = executorService.submit(task1);
		Future<PaymentResponseDto> future2 = executorService.submit(task2);

		startLatch.countDown();

		PaymentResponseDto response1 = future1.get();
		PaymentResponseDto response2 = future2.get();

		executorService.shutdown();

		long processedCount = Stream.of(response1, response2)
				.filter(response -> response.getStatus() == TransferStatus.PROCESSED)
				.count();

		long failedCount = Stream.of(response1, response2)
				.filter(response -> response.getStatus() == TransferStatus.FAILED)
				.count();

		assertEquals(1, processedCount);
		assertEquals(1, failedCount);

		Wallet updatedSource = walletRepository.findById(source.getWalletId()).orElseThrow();

		assertEquals(0, updatedSource.getBalance().compareTo(new BigDecimal("20.00")));

		assertEquals(2, transferRepository.count());
		assertEquals(2, ledgerEntryRepository.count());
	}

}

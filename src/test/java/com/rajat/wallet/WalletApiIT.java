package com.rajat.wallet;

import static org.assertj.core.api.Assertions.assertThat;

import com.rajat.wallet.dto.PageResponse;
import com.rajat.wallet.dto.TransferResponse;
import com.rajat.wallet.dto.WalletResponse;
import com.rajat.wallet.support.AbstractIntegrationTest;
import java.util.Comparator;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.core.ParameterizedTypeReference;
import org.springframework.http.HttpMethod;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

/**
 * Behaviour of the read-side endpoints: {@code GET /wallets/{id}} (balance) and {@code GET
 * /wallets/{id}/transfers} (history).
 */
class WalletApiIT extends AbstractIntegrationTest {

  @Test
  void getWalletReturnsBalanceAndCurrency() {
    UUID wallet = seedWallet("250.75", "INR");

    ResponseEntity<WalletResponse> response =
        restTemplate.getForEntity("/wallets/" + wallet, WalletResponse.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(response.getBody()).isNotNull();
    assertThat(response.getBody().walletId()).isEqualTo(wallet);
    assertThat(response.getBody().balance()).isEqualByComparingTo("250.75");
    assertThat(response.getBody().currency()).isEqualTo("INR");
  }

  @Test
  void getWalletReflectsBalanceAfterTransfer() {
    UUID source = seedWallet("100.00", "INR");
    UUID dest = seedWallet("0.00", "INR");
    postTransfer(transfer("bal-1", source, dest, "30.00"));

    assertThat(walletBalance(source)).isEqualByComparingTo("70.00");
    assertThat(walletBalance(dest)).isEqualByComparingTo("30.00");
  }

  @Test
  void getUnknownWalletReturns404() {
    ResponseEntity<String> response =
        restTemplate.getForEntity("/wallets/" + UUID.randomUUID(), String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
  }

  @Test
  void getWalletWithMalformedIdReturns400() {
    ResponseEntity<String> response =
        restTemplate.getForEntity("/wallets/not-a-uuid", String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
  }

  @Test
  void transferHistoryListsEveryTransferInvolvingTheWalletNewestFirst() {
    UUID a = seedWallet("1000.00", "INR");
    UUID b = seedWallet("0.00", "INR");
    UUID c = seedWallet("0.00", "INR");

    postTransfer(transfer("h1", a, b, "10.00")); // a is source
    postTransfer(transfer("h2", a, c, "20.00")); // a is source
    postTransfer(transfer("h3", b, a, "5.00")); //  a is destination

    PageResponse<TransferResponse> history = transferHistory(a, "");

    // All three involve wallet a (twice as source, once as destination).
    assertThat(history.totalElements()).isEqualTo(3);
    assertThat(history.content())
        .hasSize(3)
        .allSatisfy(t -> assertThat(a).isIn(t.fromWalletId(), t.toWalletId()))
        .isSortedAccordingTo(Comparator.comparing(TransferResponse::createdAt).reversed());

    // A wallet not involved in a transfer does not see it.
    assertThat(transferHistory(c, "").totalElements()).isEqualTo(1);
  }

  @Test
  void transferHistoryIsPaginated() {
    UUID a = seedWallet("1000.00", "INR");
    UUID b = seedWallet("0.00", "INR");
    postTransfer(transfer("p1", a, b, "10.00"));
    postTransfer(transfer("p2", a, b, "10.00"));
    postTransfer(transfer("p3", a, b, "10.00"));

    PageResponse<TransferResponse> first = transferHistory(a, "?page=0&size=2");
    assertThat(first.content()).hasSize(2);
    assertThat(first.totalElements()).isEqualTo(3);
    assertThat(first.totalPages()).isEqualTo(2);
    assertThat(first.first()).isTrue();
    assertThat(first.last()).isFalse();

    PageResponse<TransferResponse> second = transferHistory(a, "?page=1&size=2");
    assertThat(second.content()).hasSize(1);
    assertThat(second.first()).isFalse();
    assertThat(second.last()).isTrue();
  }

  @Test
  void transferHistoryForUnknownWalletReturns404() {
    ResponseEntity<String> response =
        restTemplate.getForEntity("/wallets/" + UUID.randomUUID() + "/transfers", String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
  }

  private java.math.BigDecimal walletBalance(UUID id) {
    return restTemplate.getForEntity("/wallets/" + id, WalletResponse.class).getBody().balance();
  }

  private PageResponse<TransferResponse> transferHistory(UUID id, String query) {
    return restTemplate
        .exchange(
            "/wallets/" + id + "/transfers" + query,
            HttpMethod.GET,
            null,
            new ParameterizedTypeReference<PageResponse<TransferResponse>>() {})
        .getBody();
  }
}

package com.wallettransfer.domain;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;

/**
 * Fingerprint of the business content of a transfer request, stored with the idempotency key so
 * that reusing a key with a different payload is detected instead of replaying an unrelated result.
 */
public final class RequestHash {

  private RequestHash() {}

  /** SHA-256 over the length-prefixed (from, to, amount), hex encoded. */
  public static String of(String fromWalletId, String toWalletId, long amount) {
    MessageDigest digest = sha256();
    for (String part : new String[] {fromWalletId, toWalletId, Long.toString(amount)}) {
      // the length prefix removes ambiguity between ("ab","c") and ("a","bc")
      digest.update((part.length() + ":" + part).getBytes(StandardCharsets.UTF_8));
    }
    return HexFormat.of().formatHex(digest.digest());
  }

  private static MessageDigest sha256() {
    try {
      return MessageDigest.getInstance("SHA-256");
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException("SHA-256 is required by the Java platform", e);
    }
  }
}

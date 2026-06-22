package com.walletTransfer.walletTransfer.util;

import com.walletTransfer.walletTransfer.dto.TransferRequest;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
public final class RequestHashUtil {

    private RequestHashUtil() {
    }

    public static String generateHash(
            TransferRequest request) {

        String payload =
                request.getFromWalletId()
                        + "|"
                        + request.getToWalletId()
                        + "|"
                        + request.getAmount();

        try {

            MessageDigest digest =
                    MessageDigest.getInstance("SHA-256");

            byte[] hash =
                    digest.digest(
                            payload.getBytes(
                                    StandardCharsets.UTF_8));

            StringBuilder hex =
                    new StringBuilder();

            for (byte b : hash) {
                hex.append(
                        String.format("%02x", b));
            }

            return hex.toString();

        } catch (NoSuchAlgorithmException ex) {

            throw new IllegalStateException(
                    "Unable to generate request hash",
                    ex
            );
        }
    }
}
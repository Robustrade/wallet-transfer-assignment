package com.example.wallettransfer.util;

import com.example.wallettransfer.dto.CreateTransferRequest;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;


public class RequestHashGenerator {

    public static String hash(CreateTransferRequest request) {
        String canonicalAmount = request.amount().stripTrailingZeros().toPlainString();
        String canonicalPayload = lengthPrefixed(request.fromWalletId())
                + lengthPrefixed(request.toWalletId())
                + lengthPrefixed(canonicalAmount);

        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            return HexFormat.of().formatHex(digest.digest(canonicalPayload.getBytes(StandardCharsets.UTF_8)));
        } catch (NoSuchAlgorithmException exception) {
            throw new IllegalStateException("SHA-256 is not available", exception);
        }
    }

    private static String lengthPrefixed(String value) {
        return value.length() + ":" + value;
    }
}

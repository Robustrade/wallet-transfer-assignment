package com.walletTransfer.walletTransfer.service.impl;

import com.walletTransfer.walletTransfer.dto.TransferRequest;
    import com.walletTransfer.walletTransfer.service.HashingService;
import org.springframework.stereotype.Service;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

@Service
public class Sha256HashingService implements HashingService{
    @Override
    public String generateHash(
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

            StringBuilder builder =
                    new StringBuilder();

            for (byte b : hash) {
                builder.append(
                        String.format("%02x", b));
            }

            return builder.toString();

        } catch (NoSuchAlgorithmException ex) {

            throw new IllegalStateException(
                    "Unable to generate request hash",
                    ex);
        }
    }

}

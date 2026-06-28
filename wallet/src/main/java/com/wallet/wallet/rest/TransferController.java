package com.wallet.wallet.rest;

import com.wallet.wallet.dto.TransferEnquiryList;
import com.wallet.wallet.dto.TransferRequestDTO;
import com.wallet.wallet.dto.TransferResponeDTO;
import com.wallet.wallet.enums.CATEGORY;
import com.wallet.wallet.enums.MONTH;
import com.wallet.wallet.enums.PAYMENT_STATUS;
import org.apache.coyote.Response;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.time.Month;

@RestController
public class TransferController {


    @PostMapping("/transfer")
    public ResponseEntity<TransferResponeDTO> saveTransfer(@RequestBody TransferRequestDTO transferRequestDTO){
        return new ResponseEntity<>(null);
    }

    @GetMapping("/wallet/{walletId}/history")
    public ResponseEntity<TransferEnquiryList> getEnquiry(@RequestParam MONTH month, @RequestParam CATEGORY category, @RequestParam PAYMENT_STATUS paymentStatus){
        return new ResponseEntity<>(null);
    }


}

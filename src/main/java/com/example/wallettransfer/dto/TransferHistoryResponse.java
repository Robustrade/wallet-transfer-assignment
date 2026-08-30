package com.example.wallettransfer.dto;

import java.util.List;

public record TransferHistoryResponse(List<TransferHistoryItem> transfers, String nextCursor) {
}

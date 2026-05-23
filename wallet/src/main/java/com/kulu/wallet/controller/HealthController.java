package com.kulu.wallet.controller;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.ResponseEntity;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

import java.time.Instant;
import java.util.Map;

@RestController
public class HealthController {

    @Autowired
    private JdbcTemplate jdbc;

    @GetMapping("/health")
    public ResponseEntity<Map<String, Object>> health() {
        try {
            jdbc.queryForObject("SELECT 1", Integer.class);
            return ResponseEntity.ok(Map.of(
                "status", "UP",
                "db",     "UP",
                "time",   Instant.now().toString()
            ));
        } catch (Exception e) {
            return ResponseEntity.status(503).body(Map.of(
                "status", "DOWN",
                "db",     "DOWN",
                "error",  e.getMessage(),
                "time",   Instant.now().toString()
            ));
        }
    }
}

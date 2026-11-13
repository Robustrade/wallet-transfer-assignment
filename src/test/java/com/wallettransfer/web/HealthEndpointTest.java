package com.wallettransfer.web;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.wallettransfer.support.PostgresIntegrationTest;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.test.web.servlet.MockMvc;

@AutoConfigureMockMvc
class HealthEndpointTest extends PostgresIntegrationTest {

  @Autowired private MockMvc mvc;

  @Test
  void reportsUpWhenTheDatabaseIsReachable() throws Exception {
    mvc.perform(get("/actuator/health"))
        .andExpect(status().isOk())
        .andExpect(jsonPath("$.status").value("UP"));
  }

  @Test
  void doesNotExposeOtherActuatorEndpoints() throws Exception {
    mvc.perform(get("/actuator/env")).andExpect(status().isNotFound());
  }
}

package com.wallettransfer.service;

import java.util.UUID;
import org.springframework.stereotype.Component;

/** Random UUIDs. */
@Component
class UuidIdGenerator implements IdGenerator {

  @Override
  public String newId() {
    return UUID.randomUUID().toString();
  }
}

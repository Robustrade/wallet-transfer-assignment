package com.rajat.wallet.domain.entities.common;

import jakarta.persistence.Id;
import jakarta.persistence.MappedSuperclass;
import java.util.UUID;
import lombok.Getter;
import org.hibernate.annotations.UuidGenerator;

/**
 * Root for all persistent entities. Supplies the primary key: a time-ordered UUID v7. Hibernate
 * 6.5's {@link UuidGenerator.Style#TIME} emits RFC 9562 version-7 UUIDs and assigns the id on
 * persist, so callers never set it. Time-ordered keys keep primary-key inserts index-friendly.
 */
@MappedSuperclass
@Getter
public abstract class BaseEntity {

  @Id
  @UuidGenerator(style = UuidGenerator.Style.TIME)
  private UUID id;
}

package com.wallettransfer.service;

/** Source of unique identifiers; a seam so tests can make ids predictable. */
@FunctionalInterface
public interface IdGenerator {

  String newId();
}

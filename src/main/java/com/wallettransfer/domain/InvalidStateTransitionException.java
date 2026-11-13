package com.wallettransfer.domain;

/** A transfer was asked to leave a terminal state or to move along a non-existent edge. */
public class InvalidStateTransitionException extends RuntimeException {

  private static final long serialVersionUID = 1L;

  public InvalidStateTransitionException(TransferState from, TransferState to) {
    super("invalid transfer state transition: " + from + " -> " + to);
  }
}

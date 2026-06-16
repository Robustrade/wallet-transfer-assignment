package com.viditec.wallettransfer.exception;

public class ResourceNotFoundException extends TransferException {

    public ResourceNotFoundException(String resourceType, String resourceId) {
        super("RESOURCE_NOT_FOUND", "%s '%s' was not found.".formatted(resourceType, resourceId));
    }
}

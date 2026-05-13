#!/bin/bash

# Exit immediately if a command exits with a non-zero status
set -e

# Stop existing containers and remove orphans to ensure a clean state
echo "Stopping existing containers..."
docker compose down --remove-orphans

# Build and start services in detached mode
echo "Building and starting services..."
docker compose up --build -d

# Follow the application logs to monitor startup
echo "Services started. Showing application logs (Ctrl+C to exit logs):"
echo "---------------------------------------------------------------"
docker compose logs -f app
# syntax=docker/dockerfile:1
#
# Multi-stage build so `docker compose up --build` produces a runnable image
# from a clean checkout without requiring a host-side Gradle build.

# ── Stage 1: build the bootable jar ──────────────────────────────────────────
FROM eclipse-temurin:17-jdk AS build
WORKDIR /workspace

# Copy the Gradle wrapper and build scripts first so the dependency-resolution
# layer can be cached when only source code changes.
COPY gradlew gradlew.bat ./
COPY gradle ./gradle
COPY settings.gradle.kts build.gradle.kts ./
RUN chmod +x gradlew && ./gradlew --version

# Now copy the sources and produce the Spring Boot fat jar.
COPY src ./src
RUN ./gradlew bootJar --no-daemon -x test

# ── Stage 2: minimal runtime image ───────────────────────────────────────────
FROM eclipse-temurin:17-jre
WORKDIR /app
COPY --from=build /workspace/build/libs/wallet-transfer-0.1.0.jar app.jar
EXPOSE 8080
ENTRYPOINT ["java","-jar","/app/app.jar"]

# syntax=docker/dockerfile:1.7
# ============================================
# Stage 1: Build
# ============================================
FROM maven:3.9-eclipse-temurin-21 AS builder

WORKDIR /app

COPY pom.xml .
COPY src/ src/

# BuildKit cache for ~/.m2: replaces `mvn dependency:go-offline`, which
# is incomplete with Spring Boot (misses test-scope and plugin
# transitives, e.g. jooq-codegen → jooq-meta-extensions). The cache lets
# the actual `mvn package` step be authoritative without re-resolving
# every build. `-Dmaven.test.skip=true` skips test compile AND execute
# (faster than `-DskipTests`); tests run via Dockerfile.test.
RUN --mount=type=cache,target=/root/.m2,sharing=locked \
    mvn -B -ntp -Dmaven.test.skip=true package

# ============================================
# Stage 2: Run
# ============================================
# alpine over distroless: keeps the in-image HEALTHCHECK (busybox wget)
# instead of bundling a Java probe. Hikari + Postgres JDBC are pure
# Java, so the alpine/musl caveat does not bite this stack.
FROM eclipse-temurin:21-jre-alpine

# CIS Docker Benchmark 4.1: never run as root in a financial workload.
# Fixed UID for stable file ownership across mounted volumes.
RUN addgroup -S appuser && adduser -S -G appuser -u 10001 appuser

WORKDIR /app

# Pinned name (pom.xml: <finalName>app</finalName>) avoids the glob's
# alphabetical-first-match foot-gun. --chown lets appuser read the JAR
# after the USER directive — COPY preserves root ownership by default.
COPY --from=builder --chown=appuser:appuser /app/target/app.jar /app/app.jar

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/actuator/health || exit 1

# JVM tuning via JAVA_TOOL_OPTIONS so an operator can override at runtime
# without a rebuild and PID 1 stays as java (no shell wrapper).
#   - ExitOnOutOfMemoryError: hard-fail on heap exhaustion, let the
#     orchestrator restart instead of serving 500s from a wedged JVM.
#   - MaxRAMPercentage=75.0: JDK 21 honours cgroup memory but its ~25%
#     default heap fraction on small containers is unhelpfully tight.
ENV JAVA_TOOL_OPTIONS="-XX:+ExitOnOutOfMemoryError -XX:MaxRAMPercentage=75.0"

ENTRYPOINT ["java", "-jar", "/app/app.jar"]

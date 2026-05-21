plugins {
    java
    checkstyle
    id("org.springframework.boot") version "3.3.5"
    id("io.spring.dependency-management") version "1.1.6"
    id("com.diffplug.spotless") version "6.25.0"
}

group = "com.kullu"
version = "0.1.0"

java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(17))
    }
}

repositories {
    mavenCentral()
}

dependencies {
    implementation("org.springframework.boot:spring-boot-starter-web")
    implementation("org.springframework.boot:spring-boot-starter-data-jpa")
    implementation("org.springframework.boot:spring-boot-starter-validation")
    implementation("org.flywaydb:flyway-core")
    implementation("org.flywaydb:flyway-database-postgresql")
    implementation("org.postgresql:postgresql")
    compileOnly("org.projectlombok:lombok")
    annotationProcessor("org.projectlombok:lombok")

    testImplementation("org.springframework.boot:spring-boot-starter-test")
    testImplementation("org.springframework.boot:spring-boot-testcontainers")
    testImplementation("org.testcontainers:junit-jupiter:1.20.3")
    testImplementation("org.testcontainers:postgresql:1.20.3")
    testCompileOnly("org.projectlombok:lombok")
    testAnnotationProcessor("org.projectlombok:lombok")
}

sourceSets {
    create("integrationTest") {
        java.srcDir("src/integrationTest/java")
        resources.srcDir("src/integrationTest/resources")
        compileClasspath += sourceSets.main.get().output
        runtimeClasspath += sourceSets.main.get().output
    }
}

val integrationTestImplementation: Configuration by configurations.getting {
    extendsFrom(configurations.testImplementation.get())
}
configurations["integrationTestRuntimeOnly"].extendsFrom(configurations.testRuntimeOnly.get())

val integrationTest = tasks.register<Test>("integrationTest") {
    description = "Runs integration & concurrency tests against Testcontainers Postgres."
    group = "verification"
    testClassesDirs = sourceSets["integrationTest"].output.classesDirs
    classpath = sourceSets["integrationTest"].runtimeClasspath
    useJUnitPlatform()
    shouldRunAfter(tasks.test)
    systemProperty("junit.jupiter.execution.parallel.enabled", "false")
}

tasks.test {
    useJUnitPlatform()
}

// ── Format: Spotless ────────────────────────────────────────────────────────
// Pragmatic, low-churn rules tuned to the existing code style. Keeps
// `./gradlew spotlessCheck` meaningful without forcing a project-wide
// reformat. Use `./gradlew spotlessApply` to fix violations automatically.
spotless {
    java {
        target("src/**/*.java")
        removeUnusedImports()
        importOrder("java", "javax", "jakarta", "org", "com", "")
        trimTrailingWhitespace()
        endWithNewline()
        indentWithSpaces(4)
    }
}

// ── Lint: Checkstyle ────────────────────────────────────────────────────────
// Custom focused ruleset at config/checkstyle/checkstyle.xml. Kept
// intentionally lean so violations indicate real problems.
checkstyle {
    toolVersion = "10.17.0"
    configFile = rootProject.file("config/checkstyle/checkstyle.xml")
    isIgnoreFailures = false
    maxWarnings = 0
}

// Checkstyle auto-creates a task per source set
// (checkstyleMain, checkstyleTest, checkstyleIntegrationTest). Nothing extra
// to register here; the `check` task already aggregates them via the
// Checkstyle plugin's wiring.

tasks.check {
    dependsOn(integrationTest)
    dependsOn("spotlessCheck")
}

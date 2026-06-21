#!/bin/sh
# Maven Wrapper script
# This script downloads and runs Maven if not already available

set -e

MAVEN_WRAPPER_PROPERTIES=".mvn/wrapper/maven-wrapper.properties"

if [ -f "$MAVEN_WRAPPER_PROPERTIES" ]; then
    DIST_URL=$(grep "distributionUrl" "$MAVEN_WRAPPER_PROPERTIES" | cut -d'=' -f2-)
fi

if [ -z "$DIST_URL" ]; then
    DIST_URL="https://repo.maven.apache.org/maven2/org/apache/maven/apache-maven/3.9.9/apache-maven-3.9.9-bin.zip"
fi

MAVEN_HOME="${HOME}/.m2/wrapper/dists/apache-maven-3.9.9"

if [ ! -d "$MAVEN_HOME" ]; then
    echo "Downloading Maven..."
    mkdir -p "$MAVEN_HOME"
    TEMP_FILE=$(mktemp)
    curl -fsSL "$DIST_URL" -o "$TEMP_FILE"
    unzip -q "$TEMP_FILE" -d "$MAVEN_HOME"
    rm "$TEMP_FILE"
fi

MVN_CMD=$(find "$MAVEN_HOME" -name "mvn" -path "*/bin/mvn" | head -1)

if [ -z "$MVN_CMD" ]; then
    echo "Error: Could not find Maven executable. Falling back to system Maven."
    MVN_CMD="mvn"
fi

exec "$MVN_CMD" "$@"


FROM eclipse-temurin:25-jdk-alpine AS build
WORKDIR /app

COPY .mvn/ .mvn/
COPY mvnw pom.xml ./
RUN ./mvnw -B dependency:go-offline

COPY src/ src/
RUN ./mvnw -B clean package -DskipTests

FROM eclipse-temurin:25-jre-alpine
WORKDIR /app

RUN addgroup -S wallet && adduser -S wallet -G wallet
COPY --from=build /app/target/*.jar app.jar
USER wallet

EXPOSE 8080
ENTRYPOINT ["java", "-jar", "app.jar"]

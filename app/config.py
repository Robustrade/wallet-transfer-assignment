from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    database_url: str = "postgresql+psycopg2://wallet:wallet@localhost:5432/wallet"
    app_name: str = "Wallet Transfer Service"


settings = Settings()

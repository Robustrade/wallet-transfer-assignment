from fastapi import FastAPI

from app.api.errors import register_exception_handlers
from app.api.routes import router
from app.config import settings
from app.db import models  # noqa: F401  (register models with the metadata)

app = FastAPI(title=settings.app_name)
register_exception_handlers(app)
app.include_router(router)


@app.get("/health")
def health():
    return {"status": "ok"}

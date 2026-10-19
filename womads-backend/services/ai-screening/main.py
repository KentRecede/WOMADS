"""AI applicant screening service (placeholder).

The paper builds the core modules first (SRS constraint 2.5.8: the AI screening
module comes last), so for now this only proves that the service runs, is
containerised, and is reachable from the Go API over the internal network.

When you build it, the Go API will call POST /v1/screen with an applicant's
resume text and the job's requirements; this service returns the extracted
criteria for the Weighted Sum Model to score.
"""
from fastapi import FastAPI, HTTPException

app = FastAPI(title="WOMADS AI Screening Service", version="0.1.0")


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}


@app.post("/v1/screen")
def screen() -> None:
    raise HTTPException(status_code=501, detail="Applicant screening is not implemented yet.")

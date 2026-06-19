"""
MCP Weather Server
-------------------
Exposes two tools via the Model Context Protocol:
  - get_forecast(latitude, longitude) : current forecast from the NWS API
  - get_alerts(state)                 : active weather alerts for a US state

Run standalone:  python weather_server.py
(Uses stdio transport so the MCP client can spawn it as a subprocess.)
"""

import json
import httpx
from mcp.server.fastmcp import FastMCP

mcp = FastMCP("weather")

NWS_API_BASE = "https://api.weather.gov"
HEADERS = {
    "User-Agent": "weather-mcp-demo/1.0",
    "Accept": "application/geo+json",
}


async def _nws_get(url: str) -> dict | None:
    """Helper: make a GET request to the NWS API."""
    async with httpx.AsyncClient() as client:
        try:
            resp = await client.get(url, headers=HEADERS, timeout=30.0)
            resp.raise_for_status()
            return resp.json()
        except (httpx.HTTPError, json.JSONDecodeError):
            return None


def _format_alert(feature: dict) -> str:
    props = feature.get("properties", {})
    return (
        f"Event:    {props.get('event', 'Unknown')}\n"
        f"Area:     {props.get('areaDesc', 'Unknown')}\n"
        f"Severity: {props.get('severity', 'Unknown')}\n"
        f"Headline: {props.get('headline', 'N/A')}\n"
        f"Description: {(props.get('description', 'N/A'))[:300]}"
    )


@mcp.tool()
async def get_forecast(latitude: float, longitude: float) -> str:
    """Get the weather forecast for a location (US only).

    Args:
        latitude: Latitude of the location (e.g. 37.7749 for San Francisco)
        longitude: Longitude of the location (e.g. -122.4194 for San Francisco)
    """
    # Step 1: resolve lat/lon to an NWS grid point
    points_url = f"{NWS_API_BASE}/points/{latitude},{longitude}"
    points_data = await _nws_get(points_url)
    if not points_data:
        return "Error: could not resolve location to an NWS grid point."

    forecast_url = points_data["properties"].get("forecast")
    if not forecast_url:
        return "Error: no forecast URL returned for this location."

    # Step 2: fetch the forecast
    forecast_data = await _nws_get(forecast_url)
    if not forecast_data:
        return "Error: could not fetch forecast data."

    periods = forecast_data["properties"].get("periods", [])[:5]
    if not periods:
        return "No forecast periods available."

    lines = []
    for p in periods:
        lines.append(
            f"{p['name']}: {p['temperature']}°{p['temperatureUnit']} — "
            f"{p['shortForecast']}"
        )
    return "\n".join(lines)


@mcp.tool()
async def get_alerts(state: str) -> str:
    """Get active weather alerts for a US state.

    Args:
        state: Two-letter US state code (e.g. TX, CA, NY)
    """
    url = f"{NWS_API_BASE}/alerts/active/area/{state.upper()}"
    data = await _nws_get(url)
    if not data:
        return "Error: could not fetch alerts."

    features = data.get("features", [])
    if not features:
        return f"No active weather alerts for {state.upper()}."

    alerts = [_format_alert(f) for f in features[:5]]
    return "\n---\n".join(alerts)


if __name__ == "__main__":
    mcp.run(transport="stdio")

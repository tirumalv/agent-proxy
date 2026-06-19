"""
Weather MCP Client (with agent-proxy debugging)
-------------------
Interactive CLI that:
  1. Spawns agent-proxy which wraps the MCP weather server as a subprocess
  2. agent-proxy captures all stdio messages for visualization
  3. Discovers MCP server tools
  4. Lets you ask weather questions in natural language
  5. Routes Claude's tool calls to the MCP server and returns results

Messages will be visible at http://localhost:7700/ui

Usage:
  export ANTHROPIC_API_KEY=sk-...
  python weather_client.py
"""

import asyncio
import json
import sys
import os

from dotenv import load_dotenv
from anthropic import Anthropic
from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client


async def main():
    load_dotenv()
    if not os.environ.get("ANTHROPIC_API_KEY"):
        print("Error: set ANTHROPIC_API_KEY environment variable first.")
        sys.exit(1)

    # --- 1. Connect to the MCP weather server via agent-proxy (stdio) ---
    # This routes the connection through agent-proxy which captures all messages
    # to display on http://localhost:7700/ui
    weather_server_cmd = f'{sys.executable} weather_server.py'
    server_params = StdioServerParameters(
        command=r"C:\work\agent-proxy\agent-proxy.exe",
        args=["stdio", "--cmd", weather_server_cmd, "--ui-port", "7700"],
        cwd=os.path.dirname(os.path.abspath(__file__)),
    )

    async with stdio_client(server_params) as (read_stream, write_stream):
        async with ClientSession(read_stream, write_stream) as session:
            await session.initialize()

            # --- 2. Discover tools ---
            tools_result = await session.list_tools()
            mcp_tools = tools_result.tools
            print(f"Connected to MCP server. Available tools: "
                  f"{[t.name for t in mcp_tools]}\n")

            # Convert MCP tool schemas to Anthropic tool format
            anthropic_tools = []
            for t in mcp_tools:
                anthropic_tools.append({
                    "name": t.name,
                    "description": t.description or "",
                    "input_schema": t.inputSchema,
                })

            # --- 3. Interactive loop ---
            client = Anthropic()
            conversation: list[dict] = []

            print("Ask me about US weather!  (type 'quit' to exit)\n")

            while True:
                user_input = input("You: ").strip()
                if not user_input:
                    continue
                if user_input.lower() in ("quit", "exit", "q"):
                    print("Goodbye!")
                    break

                conversation.append({"role": "user", "content": user_input})

                # --- 4. Send to Claude with MCP tools ---
                response = client.messages.create(
                    model="claude-opus-4-1-20250805",
                    max_tokens=1024,
                    system=(
                        "You are a helpful weather assistant. Use the available "
                        "tools to answer weather questions about US locations. "
                        "If the user gives a city name, look up appropriate "
                        "latitude/longitude coordinates to use with get_forecast."
                    ),
                    tools=anthropic_tools,
                    messages=conversation,
                )

                # --- 5. Tool-calling loop ---
                while response.stop_reason == "tool_use":
                    tool_blocks = [b for b in response.content
                                   if b.type == "tool_use"]
                    assistant_content = response.content
                    conversation.append({"role": "assistant",
                                         "content": assistant_content})

                    tool_results = []
                    for tb in tool_blocks:
                        print(f"  [Calling tool: {tb.name}({tb.input})]")
                        result = await session.call_tool(tb.name, tb.input)

                        # Collect text from the result content blocks
                        result_text = ""
                        for block in result.content:
                            if hasattr(block, "text"):
                                result_text += block.text

                        tool_results.append({
                            "type": "tool_result",
                            "tool_use_id": tb.id,
                            "content": result_text,
                        })

                    conversation.append({"role": "user", "content": tool_results})

                    # Continue conversation so Claude can interpret results
                    response = client.messages.create(
                        model="claude-opus-4-1-20250805",
                        max_tokens=1024,
                        system=(
                            "You are a helpful weather assistant. Use the "
                            "available tools to answer weather questions about "
                            "US locations."
                        ),
                        tools=anthropic_tools,
                        messages=conversation,
                    )

                # --- 6. Print final text response ---
                final_text = "".join(
                    b.text for b in response.content if hasattr(b, "text")
                )
                conversation.append({"role": "assistant",
                                     "content": response.content})
                print(f"\nAssistant: {final_text}\n")


if __name__ == "__main__":
    asyncio.run(main())

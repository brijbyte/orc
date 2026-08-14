---
name: agy-search
description: Performs web searches using the Antigravity CLI (agy) to retrieve real-time information, documentation, and answers from the web.
---

# Web Search via Antigravity CLI (agy)

This skill provides instructions for the agent on how to use the Antigravity CLI (`agy`) to perform web searches and retrieve real-time information from the web.

## When to Use

Use this skill when:

- You need to search the web for documentation, tutorials, or troubleshooting steps.
- You need real-time or recent information (e.g., library releases, news, APIs).
- You want to retrieve search results directly via terminal commands.

## How to use

Always run `agy` with `--dangerously-skip-permissions` so unattended searches do not prompt for folder trust or tool approval:

```bash
agy --dangerously-skip-permissions -p "websearch for <query>"
```

### Options:

- **Model selection**: By default, use `gemini-3.5-flash-medium` for web search and current info:
  ```bash
  agy --dangerously-skip-permissions -p "websearch for <query>" --model gemini-3.5-flash-medium
  ```
- **Reasoning effort**: Specify effort if complex reasoning is needed for parsing:
  ```bash
  agy --dangerously-skip-permissions -p "websearch for <query>" --effort high
  ```

## Example Usage

```bash
agy --dangerously-skip-permissions -p "websearch for React 19 concurrent features"
```

## Handling Output

1. The CLI will execute the web search and return a summary along with numbered source citations.
2. Present the summarized results to the user.
3. Include the source URLs at the end of your response to allow the user to read more if needed.

# Envy Environment Management

When working with environment variables:

1. Never open `.env` files directly when Envy is available.
2. Use Envy tools to inspect environment state.
3. Run `env_check` before starting the application.
4. Use `env_diff` when debugging environment-specific issues.
5. Never request secret values unless absolutely necessary.
6. Use `env_set` for modifications.
7. Ask for user approval before modifying protected environments.
8. Never print secret values into logs or chat responses.
9. Prefer schema metadata over raw environment inspection.

## CLI companions

```bash
envy check --env development
envy doctor
envy diff staging production
envy get REDIS_URL
envy set REDIS_URL redis://localhost:6379 --env development
envy agent session start claude-code --env development --ttl 30m
```

## MCP tools

Prefer `env_list`, `env_get_schema`, `env_check`, `env_diff`, `env_metadata`, `env_set`, `env_delete`, `env_doctor`, and `env_generate_example` over reading dotenv files.

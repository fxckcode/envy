# Envy

> **Secure environment management for humans, CI and AI coding agents.**

Envy es una herramienta open source para gestionar variables de entorno de forma segura desde una **TUI**, una **CLI** y un **MCP Server**, con soporte para agentes de código como Claude Code, Cursor, Codex, Copilot y otros clientes compatibles con MCP.

La idea principal es sencilla:

> Los humanos, los scripts y los agentes deberían poder saber qué configuración necesita un proyecto sin tener acceso indiscriminado a todos sus secretos.

---

## Problema

En muchos proyectos las variables de entorno terminan repartidas entre archivos como:

```text
.env
.env.local
.env.development
.env.staging
.env.production
.env.example
```

Con el tiempo aparecen problemas frecuentes:

- Variables faltantes entre ambientes.
- Secretos incluidos accidentalmente en `.env.example`.
- Diferencias difíciles de detectar entre development, staging y production.
- Valores inválidos o con tipos incorrectos.
- Agentes de código leyendo archivos `.env` completos aunque solo necesiten saber si una variable existe.
- Falta de auditoría sobre quién modificó una variable.
- Dificultad para trabajar con múltiples proveedores de secretos.

Envy propone una capa común para gestionar toda esta configuración.

---

# Objetivos

Envy debe funcionar como cuatro interfaces sobre un mismo core:

```text
                 ┌───────────────┐
                 │   Envy Core   │
                 └───────┬───────┘
                         │
          ┌──────────────┼──────────────┐
          │              │              │
          ▼              ▼              ▼
        TUI             CLI            MCP
       Humans          Scripts        Agents
                                        │
                              ┌─────────┴─────────┐
                              │                   │
                           Cursor           Claude Code
                           Codex              Copilot
```

Los cuatro objetivos principales son:

1. Facilitar la gestión de variables de entorno para desarrolladores.
2. Detectar errores y diferencias entre ambientes.
3. Reducir la exposición de secretos a agentes de IA.
4. Crear una interfaz estándar que pueda ser utilizada por humanos, CI/CD y agentes.

---

# TUI

El TUI sería la interfaz principal para humanos.

Ejemplo:

```text
┌─ ENVY ──────────────────────────────────────────────────┐
│ osborn-api                              ENV: staging     │
├──────────────────────┬──────────────────────────────────┤
│ Environments         │ Variables                        │
│                      │                                  │
│   development        │ DATABASE_URL       ●●●●●●●●      │
│ > staging            │ REDIS_URL          ●●●●●●●●      │
│   production         │ AWS_REGION         us-east-1     │
│                      │ OPENAI_API_KEY      ●●●●●●●●      │
│                      │ JWT_SECRET          ●●●●●●●●      │
│                      │                                  │
├──────────────────────┴──────────────────────────────────┤
│ 5 variables   ⚠ 2 missing   ✓ secrets hidden           │
├─────────────────────────────────────────────────────────┤
│ [a] add  [e] edit  [c] compare  [s] sync  [q] quit     │
└─────────────────────────────────────────────────────────┘
```

## Funciones del TUI

- Crear y eliminar variables.
- Editar valores.
- Ocultar secretos automáticamente.
- Cambiar entre ambientes.
- Comparar ambientes.
- Ejecutar validaciones.
- Visualizar proveedores externos.
- Revisar actividad de agentes.
- Aprobar o rechazar modificaciones solicitadas por agentes.

---

# Comparación de ambientes

Una de las funciones principales de Envy sería comparar ambientes.

```text
                    DEV        STAGING      PROD
DATABASE_URL         ✓            ✓           ✓
REDIS_URL            ✓            ✓           ✓
STRIPE_SECRET        ✓            ✓           ✗
SENTRY_DSN           ✗            ✓           ✓
DEBUG                true         false       false
```

Envy podría generar advertencias como:

```text
⚠ production is missing STRIPE_SECRET
⚠ DEBUG differs between staging and production
⚠ AWS_REGION exists only in production
```

---

# CLI

El TUI estaría acompañado por una CLI para automatización y scripts.

Ejemplos:

```bash
envy

envy list
envy check
envy doctor

envy diff staging production

envy run development -- npm run dev

envy export production
envy import .env
```

También debería permitir operar variables individuales:

```bash
envy get REDIS_URL
envy set REDIS_URL redis://localhost:6379
envy delete REDIS_URL
```

Los valores secretos deberían permanecer ocultos por defecto.

---

# Envy Doctor

`envy doctor` analizaría la salud de la configuración del proyecto.

```text
ENVIRONMENT HEALTH

✓ All required variables exist
✓ No duplicated keys
✓ .env is ignored by Git
✗ AWS_SECRET_ACCESS_KEY found in .env.example
⚠ DATABASE_URL appears to contain production credentials

Score: 78/100
```

Algunas verificaciones posibles:

- Variables requeridas faltantes.
- Variables duplicadas.
- Secretos dentro de archivos públicos.
- `.env` no incluido en `.gitignore`.
- Valores inválidos.
- Variables sin utilizar.
- Variables presentes en código pero no declaradas.
- Credenciales de producción usadas en development.
- Diferencias sospechosas entre staging y production.

---

# Configuración declarativa

Envy podría utilizar un archivo `envy.yaml` para definir ambientes, proveedores y esquemas.

```yaml
version: 1

environments:
  development:
    file: .env

  staging:
    file: .env.staging

  production:
    provider: aws-secrets-manager
    path: myapp/production

schema:
  DATABASE_URL:
    required: true
    secret: true
    type: url

  REDIS_URL:
    required: true
    secret: true
    type: url

  PORT:
    required: true
    type: integer
    default: 3000

  NODE_ENV:
    type: enum
    values:
      - development
      - staging
      - production
```

Esto permitiría tratar las variables de entorno como un pequeño **type system de configuración**.

---

# Tipos soportados

El esquema podría soportar tipos como:

```text
string
integer
float
boolean
url
email
hostname
port
enum
json
secret
```

También podrían añadirse validaciones:

```yaml
PORT:
  type: integer
  min: 1
  max: 65535

API_URL:
  type: url
  required: true

LOG_LEVEL:
  type: enum
  values:
    - debug
    - info
    - warn
    - error
```

---

# MCP Server

Una de las características más importantes de Envy sería incluir un servidor MCP.

El objetivo es permitir que agentes de código trabajen con la configuración del proyecto sin necesidad de abrir los archivos `.env` completos.

En lugar de que un agente haga:

```text
read_file(.env)
```

podría llamar:

```text
envy.check_environment("development")
```

Y recibir:

```json
{
  "status": "invalid",
  "missing": [
    "REDIS_URL",
    "SENTRY_DSN"
  ],
  "invalid": [
    {
      "key": "PORT",
      "reason": "expected integer"
    }
  ]
}
```

El agente obtiene suficiente información para resolver el problema sin conocer los secretos.

---

# MCP Tools

El MCP Server podría exponer herramientas como:

```text
env_list()
env_list_environments()
env_get_schema()
env_check()
env_diff()
env_exists()
env_metadata()
env_set()
env_delete()
env_copy()
env_generate_example()
env_doctor()
```

## Ejemplo: consultar una variable

Solicitud del agente:

```text
env_metadata("DATABASE_URL")
```

Respuesta:

```json
{
  "key": "DATABASE_URL",
  "status": "configured",
  "type": "url",
  "secret": true,
  "source": ".env.development",
  "value": "[REDACTED]"
}
```

El agente sabe que la variable existe y es válida sin conocer el valor real.

---

# Secret Firewall para agentes

Envy puede actuar como una capa entre los agentes de IA y los secretos del proyecto.

En lugar de permitir acceso directo a:

```text
.env
.env.production
.env.local
```

el agente solo accede a Envy.

Puede saber:

```text
DATABASE_URL exists
DATABASE_URL is valid
DATABASE_URL belongs to production
DATABASE_URL comes from AWS Secrets Manager
```

sin conocer necesariamente:

```text
postgres://admin:super-secret-password@production...
```

---

# Permisos para agentes

Envy debería utilizar un sistema de permisos explícito.

Por defecto:

```text
READ METADATA    ✓
READ VALUES      ✗
WRITE VALUES     ✗
DELETE VALUES    ✗
```

Los permisos podrían configurarse por agente y ambiente.

Ejemplo:

```yaml
agents:
  claude-code:
    development:
      metadata: true
      read_values: false
      write: true
      delete: false

    production:
      metadata: true
      read_values: false
      write: false
      delete: false
```

---

# Solicitudes de aprobación

Cuando un agente intente modificar una variable protegida, Envy podría pedir aprobación mediante el TUI.

```text
┌─ Agent Permission Request ───────────────────────┐
│                                                 │
│ Claude Code wants to modify:                    │
│                                                 │
│ development.REDIS_URL                           │
│                                                 │
│ Old value: ********                             │
│ New value: redis://localhost:6379               │
│                                                 │
│ Reason: Redis connection is missing.            │
│                                                 │
│ [a] Allow once                                  │
│ [p] Allow for this project                      │
│ [d] Deny                                        │
└─────────────────────────────────────────────────┘
```

---

# Acceso temporal para agentes

También sería interesante permitir permisos temporales.

```bash
envy agent grant claude-code \
  --env development \
  --write \
  --ttl 30m
```

Salida:

```text
Claude Code
Environment: development

read metadata     ✓
read secrets      ✗
write             ✓
delete            ✗

Expires in: 29m 43s
```

Y posteriormente:

```bash
envy agent revoke claude-code
```

---

# Auditoría

Todas las operaciones realizadas por humanos, scripts o agentes podrían registrarse.

```text
Agent Activity

12:31 Claude Code checked development
12:32 Claude Code requested REDIS_URL metadata
12:33 Claude Code changed PORT
12:34 Claude Code ran environment validation
12:34 Environment healthy ✓
```

La auditoría nunca debería guardar secretos completos.

Podría almacenar:

- Actor.
- Tipo de operación.
- Variable afectada.
- Ambiente.
- Timestamp.
- Resultado.
- Hash opcional del valor anterior y nuevo.

---

# Agent Skill

El proyecto también podría incluir una skill reutilizable para agentes.

```text
skills/
└── env-management/
    └── SKILL.md
```

Ejemplo de reglas:

```markdown
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
```

---

# Ejemplo de uso con un coding agent

El desarrollador podría decir:

```text
Configura el proyecto para correr localmente.
```

El agente haría algo similar a:

```text
→ inspect repository
→ envy_get_schema()
→ envy_check("development")
```

Resultado:

```text
Missing:
REDIS_URL
DATABASE_URL
```

Después inspecciona `docker-compose.yml` y detecta:

```text
PostgreSQL :5432
Redis :6379
```

El agente propone:

```text
I found two missing environment variables.

DATABASE_URL = postgresql://...
REDIS_URL = redis://localhost:6379

Approve configuration?
```

Con autorización:

```text
→ env_set(...)
→ env_set(...)
→ env_check()
```

Resultado:

```text
✓ Environment ready
```

---

# Providers

Envy debería abstraer diferentes proveedores mediante una interfaz común.

Primera etapa:

```text
.env files
```

Después podrían añadirse:

```text
AWS Secrets Manager
HashiCorp Vault
1Password
Doppler
Azure Key Vault
Google Secret Manager
Kubernetes Secrets
Docker Secrets
```

Ejemplo conceptual:

```go
type SecretProvider interface {
    List(ctx context.Context) ([]SecretMetadata, error)
    Exists(ctx context.Context, key string) (bool, error)
    Get(ctx context.Context, key string) (SecretValue, error)
    Set(ctx context.Context, key string, value SecretValue) error
    Delete(ctx context.Context, key string) error
}
```

---

# Arquitectura propuesta

Go sería una buena elección para el proyecto debido a su distribución sencilla mediante binarios, concurrencia, soporte multiplataforma y buen ecosistema para herramientas CLI/TUI.

```text
envy/
├── cmd/
│   ├── envy/
│   │   └── main.go
│   │
│   └── envy-mcp/
│       └── main.go
│
├── internal/
│   ├── environments/
│   ├── schema/
│   ├── secrets/
│   ├── validators/
│   ├── providers/
│   ├── policy/
│   ├── audit/
│   └── agents/
│
├── tui/
│   ├── components/
│   ├── views/
│   └── styles/
│
├── mcp/
│   ├── tools/
│   ├── resources/
│   └── prompts/
│
├── skills/
│   └── env-management/
│       └── SKILL.md
│
├── docs/
│
├── examples/
│
├── envy.yaml
└── README.md
```

---

# Stack sugerido

## Lenguaje

```text
Go
```

## TUI

```text
Bubble Tea
Lip Gloss
Bubbles
```

## Configuración

```text
YAML
```

## Seguridad

```text
OS keychain / keyring
Encrypted local store
Scoped agent permissions
Audit log
```

## Integraciones

```text
Model Context Protocol
AWS SDK
Vault API
1Password CLI / Connect
```

---

# Posible arquitectura interna

```text
                  envy.yaml
                      │
                      ▼
               ┌─────────────┐
               │ Config Core │
               └──────┬──────┘
                      │
           ┌──────────┼───────────┐
           │          │           │
           ▼          ▼           ▼
        Schema     Policies    Providers
           │          │           │
           └──────────┼───────────┘
                      │
                      ▼
                Environment API
                      │
        ┌─────────────┼──────────────┐
        │             │              │
        ▼             ▼              ▼
       TUI           CLI            MCP
                                      │
                                      ▼
                                AI Coding Agents
```

---

# Seguridad

La seguridad debería formar parte del diseño desde el principio.

Principios:

### Secret values are opt-in

Los consumidores reciben metadata por defecto, no valores.

### Least privilege

Cada agente recibe únicamente los permisos necesarios.

### Production is protected

Los ambientes sensibles requieren autorización explícita.

### No secrets in logs

Todos los valores sensibles deben ser redactados antes de escribir logs.

### Explicit trust boundary

Los agentes nunca deberían asumir que pueden abrir `.env` directamente.

---

# MVP

El MVP debería ser pequeño y usable.

## v0.1

Soporte para:

```text
✓ Detectar archivos .env
✓ Listar variables
✓ Ocultar secretos
✓ Crear y editar variables
✓ Comparar dos ambientes
✓ envy.yaml
✓ Validaciones básicas
✓ envy check
✓ envy doctor
✓ TUI
✓ CLI
```

El objetivo de esta versión sería resolver correctamente la gestión local de `.env`.

---

# v0.2 — Agent Ready

```text
✓ MCP Server
✓ env_list
✓ env_get_schema
✓ env_check
✓ env_diff
✓ env_metadata
✓ env_set
✓ env_delete
✓ Redacción automática de secretos
✓ Permisos básicos para agentes
```

---

# v0.3 — Security Layer

```text
✓ Agent grants
✓ TTL permissions
✓ Approval requests
✓ Audit log
✓ Protected environments
✓ Secret access policies
```

---

# v0.4 — Secret Providers

```text
✓ AWS Secrets Manager
✓ HashiCorp Vault
✓ 1Password
✓ Doppler
```

---

# v1.0

Una primera versión estable podría incluir:

```text
TUI
CLI
MCP Server
Agent Skill
Schema system
Secret providers
Policy engine
Audit log
Agent permissions
CI integration
```

---

# Ideas futuras

## Automatic environment discovery

Analizar el código y detectar variables utilizadas:

```javascript
process.env.DATABASE_URL
process.env.REDIS_URL
process.env.JWT_SECRET
```

Después compararlas con `envy.yaml`.

---

## Generate schema

```bash
envy schema generate
```

Podría inspeccionar el repositorio y generar:

```yaml
schema:
  DATABASE_URL:
    required: true

  REDIS_URL:
    required: true

  JWT_SECRET:
    required: true
    secret: true
```

---

## Generate `.env.example`

```bash
envy example generate
```

Resultado:

```text
DATABASE_URL=
REDIS_URL=
PORT=3000
JWT_SECRET=
```

Sin riesgo de incluir secretos reales.

---

## CI Mode

Ejemplo:

```bash
envy check --env production --ci
```

Podría fallar el pipeline si:

```text
required variable missing
invalid environment configuration
secret leaked into example file
production policy violation
```

---

## Git Hooks

```bash
envy hooks install
```

Antes de cada commit podría detectar:

```text
.env accidentally staged
secret inside README
secret inside .env.example
```

---

## Agent Sessions

```bash
envy agent session start claude-code
```

Podría crear una sesión limitada:

```text
Agent: Claude Code
Project: API
Environment: development
Access: metadata + write
TTL: 30 minutes
```

---

# Posicionamiento

Envy no debería venderse simplemente como:

> A TUI for `.env` files.

Eso limita demasiado el proyecto.

Una mejor descripción sería:

> **Envy is the environment layer between your applications, developers and AI coding agents.**

O:

> **Secure environment management for humans, CI and AI agents.**

Otra frase interesante para el README:

> **Stop giving AI agents your entire `.env`.**

---

# Filosofía

Envy no busca reemplazar directamente herramientas como Vault o AWS Secrets Manager.

Su objetivo es proporcionar una capa común encima de ellas.

```text
                Developers
                    │
                    ▼
                   Envy
                    │
      ┌─────────────┼─────────────┐
      │             │             │
      ▼             ▼             ▼
    .env          Vault          AWS
                              Secrets Manager
```

Esto permite que tanto humanos como agentes utilicen la misma interfaz independientemente del lugar donde viven los secretos.

---

# Por qué puede funcionar como proyecto open source

El proyecto tiene varias áreas donde la comunidad puede contribuir de forma independiente:

```text
providers
validators
TUI components
MCP tools
agent integrations
secret scanners
framework detectors
shell integrations
CI integrations
```

Ejemplos de contribuciones:

```text
feat(provider): add Azure Key Vault
feat(provider): add Google Secret Manager
feat(detector): add Laravel environment discovery
feat(detector): add Django environment discovery
feat(mcp): add schema validation tool
feat(tui): add environment comparison view
```

Esto crea una arquitectura naturalmente extensible para un proyecto comunitario.

---

# Nombre

Nombre provisional:

```text
Envy
```

Tiene relación directa con:

```text
env
environment
envy
```

Alternativas posibles:

```text
envctl
envio
envdock
envguard
envkit
vaultenv
```

Pero `envy` tiene la ventaja de ser corto, memorable y fácil de escribir en terminal.

---

# Resumen

Envy sería una herramienta open source que combina:

```text
TUI
+
CLI
+
Environment schema
+
Secret providers
+
MCP Server
+
Agent Skill
+
Permission system
+
Audit log
```

El objetivo final sería convertir las variables de entorno en una interfaz estructurada y segura tanto para desarrolladores como para agentes de IA.

```text
Human
   │
   ├── TUI
   │
Scripts
   │
   ├── CLI
   │
AI Agents
   │
   └── MCP
        │
        ▼
      ENVY
        │
        ▼
 Environment + Secrets
```


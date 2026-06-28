# Technical Architecture - tsqlnotes

This document outlines the software layers and database routing maps of `tsqlnotes`.

## Architecture Diagram

```mermaid
graph TD
    A[HTTP Request] --> B[Web Adapter Handler]
    B --> C[Notes Domain Service]
    C --> D[Database Port]
    D --> E[Postgres Driver]
    D --> F[MySQL Driver]
    D --> G[MSSQL Driver]
    D --> H[SQLite Driver]
    B --> I[HTML Template Engine]
```

- **Web Adapter Handler:** Uses Go standard `http.ServeMux` to handle REST routes and static files.
- **Notes Domain Service:** Encapsulates business logic for managing notes metadata and storage structures.
- **HTML Template Engine:** Renders dynamic pages using standard Go `html/template` formats.

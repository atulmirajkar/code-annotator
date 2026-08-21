# Browser fixture

```mermaid
sequenceDiagram
    actor Reviewer
    participant API
    Reviewer->>API: Load annotations
    API-->>Reviewer: Current review
```

```text
Reviewer       API
   |            |
   |-- Load --->|
   |<-- Data ---|
```

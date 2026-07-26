CREATE TABLE operations_schema (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    identity TEXT NOT NULL,
    version INTEGER NOT NULL
);

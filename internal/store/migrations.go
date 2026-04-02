package store

const schema = `
CREATE TABLE IF NOT EXISTS zappi_status (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    serial           TEXT    NOT NULL,
    timestamp        TEXT    NOT NULL,
    grid_w           REAL    NOT NULL DEFAULT 0,
    generation_w     REAL    NOT NULL DEFAULT 0,
    diversion_w      REAL    NOT NULL DEFAULT 0,
    voltage          REAL,
    frequency        REAL,
    charge_added_kwh REAL    NOT NULL DEFAULT 0,
    zappi_mode       INTEGER NOT NULL DEFAULT 0,
    status           INTEGER NOT NULL DEFAULT 0,
    plug_status      TEXT,
    ectp1_w          REAL,
    ectp2_w          REAL,
    ectp3_w          REAL,
    ectt1            TEXT,
    ectt2            TEXT,
    ectt3            TEXT,
    UNIQUE(serial, timestamp)
);

CREATE INDEX IF NOT EXISTS idx_status_serial_ts ON zappi_status(serial, timestamp);

CREATE TABLE IF NOT EXISTS zappi_minute (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    serial      TEXT    NOT NULL,
    timestamp   TEXT    NOT NULL,
    import_j    REAL    NOT NULL DEFAULT 0,
    export_j    REAL    NOT NULL DEFAULT 0,
    gen_pos_j   REAL    NOT NULL DEFAULT 0,
    gen_neg_j   REAL    NOT NULL DEFAULT 0,
    h1d_j       REAL    NOT NULL DEFAULT 0,
    h2d_j       REAL    NOT NULL DEFAULT 0,
    h3d_j       REAL    NOT NULL DEFAULT 0,
    h1b_j       REAL    NOT NULL DEFAULT 0,
    h2b_j       REAL    NOT NULL DEFAULT 0,
    h3b_j       REAL    NOT NULL DEFAULT 0,
    voltage     REAL,
    frequency   REAL,
    UNIQUE(serial, timestamp)
);

CREATE INDEX IF NOT EXISTS idx_minute_serial_ts ON zappi_minute(serial, timestamp);

CREATE TABLE IF NOT EXISTS zappi_hourly (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    serial          TEXT    NOT NULL,
    hour_start      TEXT    NOT NULL,
    import_kwh      REAL    NOT NULL DEFAULT 0,
    export_kwh      REAL    NOT NULL DEFAULT 0,
    generation_kwh  REAL    NOT NULL DEFAULT 0,
    diverted_kwh    REAL    NOT NULL DEFAULT 0,
    boosted_kwh     REAL    NOT NULL DEFAULT 0,
    UNIQUE(serial, hour_start)
);

CREATE INDEX IF NOT EXISTS idx_hourly_serial_hour ON zappi_hourly(serial, hour_start);

CREATE TABLE IF NOT EXISTS zappi_daily (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    serial               TEXT    NOT NULL,
    date                 TEXT    NOT NULL,
    import_kwh           REAL    NOT NULL DEFAULT 0,
    export_kwh           REAL    NOT NULL DEFAULT 0,
    generation_kwh       REAL    NOT NULL DEFAULT 0,
    diverted_kwh         REAL    NOT NULL DEFAULT 0,
    boosted_kwh          REAL    NOT NULL DEFAULT 0,
    self_consumption_pct REAL,
    peak_generation_w    REAL,
    peak_import_w        REAL,
    UNIQUE(serial, date)
);

CREATE INDEX IF NOT EXISTS idx_daily_serial_date ON zappi_daily(serial, date);

CREATE TABLE IF NOT EXISTS backfill_state (
    serial      TEXT    PRIMARY KEY,
    oldest_date TEXT    NOT NULL,
    newest_date TEXT    NOT NULL,
    status      TEXT    NOT NULL DEFAULT 'in_progress',
    last_error  TEXT,
    updated_at  TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS export_state (
    export_type TEXT    PRIMARY KEY,
    last_date   TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT    NOT NULL
);
`

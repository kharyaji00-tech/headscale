PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
-- v0.22-era database, created before gormigrate was introduced in 0.23.0.
-- It has legacy core tables (users, machines, pre_auth_keys, api_keys,
-- routes, kv) and real data, but NO migrations table and NO
-- database_versions table. checkMinimumMigration must reject it with
-- errDatabaseTooOld rather than treating it as a fresh database.
CREATE TABLE `users` (`id` integer PRIMARY KEY AUTOINCREMENT,`created_at` datetime,`updated_at` datetime,`deleted_at` datetime,`name` text);
INSERT INTO users VALUES(1,'2023-06-01 10:00:00','2023-06-01 10:00:00',NULL,'alice');
INSERT INTO users VALUES(2,'2023-06-01 11:00:00','2023-06-01 11:00:00',NULL,'bob');
CREATE TABLE `machines` (`id` integer PRIMARY KEY AUTOINCREMENT,`machine_key` text,`node_key` text,`disco_key` text,`ip_addresses` text,`hostname` text,`given_name` varchar(63),`user_id` integer,`register_method` text,`forced_tags` text,`auth_key_id` integer,`last_seen` datetime,`expiry` datetime,`created_at` datetime,`updated_at` datetime,`deleted_at` datetime);
INSERT INTO machines VALUES(1,'mkey1','nkey1','dkey1','100.64.0.1','alice-laptop','alice-laptop',1,'1','',NULL,'2023-06-01 12:00:00',NULL,'2023-06-01 10:30:00','2023-06-01 10:30:00',NULL);
INSERT INTO machines VALUES(2,'mkey2','nkey2','dkey2','100.64.0.2','bob-desktop','bob-desktop',2,'1','',NULL,'2023-06-01 12:00:00',NULL,'2023-06-01 11:30:00','2023-06-01 11:30:00',NULL);
INSERT INTO machines VALUES(3,'mkey3','nkey3','dkey3','100.64.0.3','shared-server','shared-server',1,'1','',NULL,'2023-06-01 12:00:00',NULL,'2023-06-01 11:45:00','2023-06-01 11:45:00',NULL);
CREATE TABLE `pre_auth_keys` (`id` integer PRIMARY KEY AUTOINCREMENT,`key` text,`user_id` integer,`reusable` numeric,`ephemeral` numeric DEFAULT false,`used` numeric DEFAULT false,`tags` text,`created_at` datetime,`expiration` datetime);
INSERT INTO pre_auth_keys VALUES(1,'pak1',1,false,false,false,NULL,'2023-06-01 10:00:00',NULL);
CREATE TABLE `api_keys` (`id` integer PRIMARY KEY AUTOINCREMENT,`prefix` text,`hash` blob,`created_at` datetime,`expiration` datetime,`last_seen` datetime);
INSERT INTO api_keys VALUES(1,'prefix1',x'deadbeef','2023-06-01 10:00:00',NULL,NULL);
CREATE TABLE `routes` (`id` integer PRIMARY KEY AUTOINCREMENT,`machine_id` integer,`prefix` text,`created_at` datetime,`updated_at` datetime,`deleted_at` datetime);
CREATE TABLE `kv` (`k` text PRIMARY KEY,`v` text);
DELETE FROM sqlite_sequence;
INSERT INTO sqlite_sequence VALUES('users',2);
INSERT INTO sqlite_sequence VALUES('machines',3);
INSERT INTO sqlite_sequence VALUES('pre_auth_keys',1);
INSERT INTO sqlite_sequence VALUES('api_keys',1);
CREATE INDEX `idx_users_deleted_at` ON `users`(`deleted_at`);
CREATE INDEX `idx_machines_deleted_at` ON `machines`(`deleted_at`);
CREATE INDEX `idx_routes_deleted_at` ON `routes`(`deleted_at`);
COMMIT;

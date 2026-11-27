-- Macros of the analytics schema, created before the models (dbt: the
-- on-run-start hooks of dbt_project.yml carry the same statements; a test
-- checks that both agree). They are stored in the analytics database, so
-- consumers can use them too.

-- tr_hex64(h): 16 hex digits as a signed 64-bit integer (two's complement).
CREATE OR REPLACE MACRO tr_hex64(h) AS
	(CAST(('0x' || h)::UBIGINT AS HUGEINT) - CASE WHEN ('0x' || h)::UBIGINT >= 9223372036854775808 THEN CAST(18446744073709551616 AS HUGEINT) ELSE CAST(0 AS HUGEINT) END)::BIGINT;

-- tr_key(s): surrogate key of a natural key, s = concat_ws(chr(31),
-- dimension, parts...) with every part non-NULL (concat_ws skips NULLs).
-- First 8 bytes of md5 as a signed integer; 0 is reserved and maps to 1.
-- PostgreSQL: ('x' || substr(md5(s), 1, 16))::bit(64)::bigint. Go: star.Key.
CREATE OR REPLACE MACRO tr_key(s) AS
	CASE WHEN tr_hex64(substr(md5(s), 1, 16)) = 0 THEN 1 ELSE tr_hex64(substr(md5(s), 1, 16)) END;

-- tr_tilde(p, home): home directory at the start of p as "~", so the same
-- project on hosts with different home directories gets one key and no user
-- name ends up in the analytics database. Go: star.Tilde.
CREATE OR REPLACE MACRO tr_tilde(p, home) AS
	CASE WHEN home = '' OR p = '' THEN p
	     WHEN p = home OR p = rtrim(home, '/') THEN '~'
	     WHEN starts_with(p, rtrim(home, '/') || '/') THEN '~' || substr(p, length(rtrim(home, '/')) + 1)
	     ELSE p END;

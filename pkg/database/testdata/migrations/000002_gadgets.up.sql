-- The second step both creates a table and alters the first, so the test can tell "both
-- files ran" from "they ran in order".
create table gadgets (
    id bigint generated always as identity primary key
);

alter table widgets add column label text;

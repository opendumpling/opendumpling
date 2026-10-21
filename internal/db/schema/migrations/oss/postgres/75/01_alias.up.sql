-- Copyright (c) HashiCorp, Inc.
-- SPDX-License-Identifier: MPL-2.0

begin;

  create table alias (
    public_id wt_public_id primary key,
    scope_id wt_scope_id not null
      references iam_scope(public_id)
      on delete cascade
      on update cascade,
    name wt_name,
    value wt_name not null
      constraint value_must_not_be_empty
      check(length(trim(value)) > 0),
    description wt_description,
    destination_id wt_public_id not null,
    create_time wt_timestamp,
    update_time wt_timestamp,
    version wt_version,

    -- an alias value only has to be unique within its scope
    constraint alias_scope_id_value_uq unique (scope_id, value),

    -- add unique index so a composite fk can be declared.
    constraint alias_scope_id_public_id_uq unique (scope_id, public_id)
  );
  comment on table alias is
    'alias entries point a short, DNS-like value at a destination resource so it can be reached by name.';

  create trigger update_version_column after update on alias
    for each row execute procedure update_version_column();

  create trigger update_time_column before update on alias
    for each row execute procedure update_time_column();

  create trigger default_create_time_column before insert on alias
    for each row execute procedure default_create_time();

  create trigger immutable_columns before update on alias
    for each row execute procedure immutable_columns('public_id', 'create_time', 'scope_id');

  -- allow operations on alias to be oplogged
  insert into oplog_ticket
    (name, version)
  values
    ('alias', 1);

  -- alias_destination_id_valid() ensures the alias's destination exists and is a
  -- target. A target is stored in the target table; the id prefix carries the
  -- target type.
  create or replace function alias_destination_id_valid() returns trigger
  as $$
  begin
    if not exists (
      select 1
        from target
       where target.public_id = new.destination_id
    ) then
      raise exception 'destination_id % is not a valid target', new.destination_id;
    end if;
    return new;
  end;
  $$ language plpgsql;
  comment on function alias_destination_id_valid() is
    'function used in before insert and before update triggers to validate that an alias points at an existing target';

  create trigger alias_destination_id_valid before insert on alias
    for each row execute procedure alias_destination_id_valid();

  create trigger alias_destination_id_valid_update before update of destination_id on alias
    for each row execute procedure alias_destination_id_valid();

commit;

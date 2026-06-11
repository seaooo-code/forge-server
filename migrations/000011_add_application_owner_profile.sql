alter table applications
    add column if not exists owner_email text not null default '',
    add column if not exists owner_avatar_url text not null default '';

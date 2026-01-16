--
-- PostgreSQL database dump
--

-- Dumped from database version 13.3 (Debian 13.3-1)
-- Dumped by pg_dump version 13.3 (Debian 13.3-1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: pg_trgm; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;


--
-- Name: EXTENSION pg_trgm; Type: COMMENT; Schema: -; Owner: 
--

COMMENT ON EXTENSION pg_trgm IS 'text similarity measurement and index searching based on trigrams';


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: api_keys; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.api_keys (
    id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    user_id uuid,
    key_id text NOT NULL,
    key_hash text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone
);


ALTER TABLE public.api_keys OWNER TO postgres;

--
-- Name: app_users; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.app_users (
    id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    username text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.app_users OWNER TO postgres;

--
-- Name: cmd_event_tags; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.cmd_event_tags (
    tenant_id uuid NOT NULL,
    event_id bigint NOT NULL,
    tag text NOT NULL
);


ALTER TABLE public.cmd_event_tags OWNER TO postgres;

--
-- Name: cmd_events; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.cmd_events (
    seq bigint NOT NULL,
    id bigint NOT NULL,
    tenant_id uuid NOT NULL,
    ts_client timestamp with time zone,
    session_id text NOT NULL,
    host_fqdn text NOT NULL,
    cwd text,
    cmd text,
    ts_ingested timestamp with time zone DEFAULT now() NOT NULL,
    src_ip inet,
    transport text DEFAULT 'tcp-clear'::text NOT NULL,
    parse_ok boolean DEFAULT true NOT NULL,
    raw_line text NOT NULL
);


ALTER TABLE public.cmd_events OWNER TO postgres;

--
-- Name: cmd_events_id_seq; Type: SEQUENCE; Schema: public; Owner: postgres
--

ALTER TABLE public.cmd_events ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.cmd_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: tenants; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.tenants (
    id uuid NOT NULL,
    name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.tenants OWNER TO postgres;

--
-- Name: api_keys api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);


--
-- Name: api_keys api_keys_tenant_id_key_id_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_tenant_id_key_id_key UNIQUE (tenant_id, key_id);


--
-- Name: app_users app_users_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.app_users
    ADD CONSTRAINT app_users_pkey PRIMARY KEY (id);


--
-- Name: app_users app_users_tenant_id_username_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.app_users
    ADD CONSTRAINT app_users_tenant_id_username_key UNIQUE (tenant_id, username);


--
-- Name: cmd_event_tags cmd_event_tags_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.cmd_event_tags
    ADD CONSTRAINT cmd_event_tags_pkey PRIMARY KEY (tenant_id, event_id, tag);


--
-- Name: cmd_events cmd_events_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.cmd_events
    ADD CONSTRAINT cmd_events_pkey PRIMARY KEY (id);


--
-- Name: cmd_events cmd_events_tenant_id_seq_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.cmd_events
    ADD CONSTRAINT cmd_events_tenant_id_seq_key UNIQUE (tenant_id, seq);


--
-- Name: tenants tenants_id_unique; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_id_unique UNIQUE (id);


--
-- Name: tenants tenants_name_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_name_key UNIQUE (name);


--
-- Name: tenants tenants_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_pkey PRIMARY KEY (id);


--
-- Name: cmd_events_cmd_trgm; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX cmd_events_cmd_trgm ON public.cmd_events USING gin (cmd public.gin_trgm_ops);


--
-- Name: cmd_events_raw_trgm; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX cmd_events_raw_trgm ON public.cmd_events USING gin (raw_line public.gin_trgm_ops);


--
-- Name: cmd_events_tenant_id_id_desc; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX cmd_events_tenant_id_id_desc ON public.cmd_events USING btree (tenant_id, id DESC);


--
-- Name: api_keys api_keys_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id);


--
-- Name: api_keys api_keys_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.app_users(id);


--
-- Name: app_users app_users_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.app_users
    ADD CONSTRAINT app_users_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id);


--
-- Name: cmd_event_tags cmd_event_tags_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.cmd_event_tags
    ADD CONSTRAINT cmd_event_tags_event_id_fkey FOREIGN KEY (event_id) REFERENCES public.cmd_events(id) ON DELETE CASCADE;


--
-- Name: cmd_event_tags cmd_event_tags_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.cmd_event_tags
    ADD CONSTRAINT cmd_event_tags_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id);


--
-- Name: cmd_events cmd_events_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.cmd_events
    ADD CONSTRAINT cmd_events_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id);


--
-- PostgreSQL database dump complete
--


"""Streamlit-Weboberfläche für den Wiki-rog Chat.

Start:  streamlit run app.py
"""
from __future__ import annotations

import streamlit as st

from config import Config
from src.rag import RagEngine

st.set_page_config(page_title="Wiki-rog", page_icon="📚", layout="centered")


@st.cache_resource(show_spinner=False)
def get_engine() -> RagEngine:
    return RagEngine(Config.load())


st.title("📚 Wiki-rog")
st.caption("Fragen zum BookStack-Wiki – lokal via Ollama, Antworten nur aus dem Wiki.")

engine = get_engine()

if "messages" not in st.session_state:
    st.session_state.messages = []

for msg in st.session_state.messages:
    with st.chat_message(msg["role"]):
        st.markdown(msg["content"])
        if msg.get("sources"):
            with st.expander("Quellen"):
                st.markdown(msg["sources"])

question = st.chat_input("Frag das Wiki …")
if question:
    st.session_state.messages.append({"role": "user", "content": question})
    with st.chat_message("user"):
        st.markdown(question)

    with st.chat_message("assistant"):
        placeholder = st.empty()
        try:
            stream, sources = engine.answer_stream(question)
            answer = ""
            for token in stream:
                answer += token
                placeholder.markdown(answer + "▌")
            placeholder.markdown(answer)
            sources_md = engine.format_sources(sources) if sources else ""
            if sources_md:
                with st.expander("Quellen"):
                    st.markdown(sources_md)
        except Exception as exc:  # noqa: BLE001 – UI soll Fehler sichtbar machen
            answer = f"Fehler: {exc}"
            sources_md = ""
            placeholder.error(answer)

    st.session_state.messages.append(
        {"role": "assistant", "content": answer, "sources": sources_md}
    )

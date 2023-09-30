from flask import Flask, request, jsonify
from biopandas.pdb import PandasPdb
import psycopg2
import subprocess
from rmsd import rmsd
from flask import Flask, request, jsonify
from transformers import BertTokenizer, BertForNextSentencePrediction
import torch
import re
#TODO: add rmsdhk
app = Flask(__name__)

tokenizer = BertTokenizer.from_pretrained("bert-base-cased")
model = BertForNextSentencePrediction.from_pretrained("bert-base-cased").eval()


def next_prob(prompt, next_sentence, tokenizer, model, b=1.2) -> float:
    encoding = tokenizer(prompt, next_sentence, return_tensors="pt")
    logits = model(**encoding, labels=torch.LongTensor([1])).logits
    pos = b ** logits[0, 0]
    neg = b ** logits[0, 1]
    return float(pos / (pos + neg))


def sentence_check(sentences, tokenizer, model):
    sentences = re.sub(r"\([^)]*\)|\{[^}]*\}|\\|\[.*?\]", "", sentences)
    sentence = [(s + "." if s[-1] != "." else s) for s in sentences.split(". ")]
    res = ""
    final_score = 0
    sentences_len = len(sentence)
    for i in range(sentences_len - 1):
        score = next_prob(
            prompt=sentence[i],
            next_sentence=sentence[i + 1],
            tokenizer=tokenizer,
            model=model,
        )
        res += sentence[i] + " "
        final_score += score / sentences_len
        res += f"\n<# {score:.2f} #> \n"
    res += sentence[-1]
    return res


@app.route("/review_sentence", methods=["POST"])
def check_sentence():
    data = request.json
    sentences = data.get("sentences")
    result = sentence_check(sentences, tokenizer, model)
    return jsonify({"result": result})


@app.route("/movie_rec", methods=["POST"])
def movie_rec():
    data = request.json
    userid = data.get("userid")
    result = ["Paddington 2"]  # TODO: useridに基づいてレコメンド結果を格納
    return jsonify({"result": result})


def connect_to_db():
    return psycopg2.connect(
        dbname="movie_hisotry", user="", password="", host="localhost", port=""
    )


def add_movie(userid, moviename):
    conn = connect_to_db()
    cur = conn.cursor()
    try:
        cur.execute(
            "INSERT INTO movies (userid, moviename) VALUES (%s, %s)",
            (userid, moviename),
        )
        conn.commit()
    except Exception as e:
        print(f"Error: {e}")
    finally:
        cur.close()
        conn.close()


def fetch_pdb_data(pdb_id):
    return PandasPdb().fetch_pdb(pdb_id)


def compute_rmsd(pdb_id1, pdb_id2):
    pdb_data1 = (
        fetch_pdb_data(pdb_id1).df["ATOM"].drop_duplicates(subset=["residue_number"])
    )
    pdb_data2 = (
        fetch_pdb_data(pdb_id2).df["ATOM"].drop_duplicates(subset=["residue_number"])
    )
    pdb_data1 = pdb_data1[pdb_data1["chain_id"] == pdb_data1["chain_id"].unique()[0]]
    pdb_data2 = pdb_data2[pdb_data2["chain_id"] == pdb_data2["chain_id"].unique()[0]]
    merged_data = pdb_data1.merge(pdb_data2, on="residue_number", suffixes=("_1", "_2"))
    coords_pdb1 = merged_data[["x_coord_1", "y_coord_1", "z_coord_1"]].values
    coords_pdb2 = merged_data[["x_coord_2", "y_coord_2", "z_coord_2"]].values
    rmsd_value = rmsd(coords_pdb1, coords_pdb2)
    rmsdhk = subprocess.run(["./mphk", pdb_id1, pdb_id2])

    return rmsd_value


@app.route("/calculate_rmsd", methods=["POST"])
def calculate_rmsd_route():
    data = request.get_json()
    pdb_id1 = data.get("pdbID1")
    pdb_id2 = data.get("pdbID2")

    try:
        rmsd_value = compute_rmsd(pdb_id1, pdb_id2)
        return jsonify({"rmsd": rmsd_value})
    except Exception as e:
        return jsonify({"error": str(e)}), 400


if __name__ == "__main__":
    app.run(debug=True)

"""FlowForge Python SDK package setup."""

from setuptools import setup, find_packages

with open("README.md", encoding="utf-8") as f:
    long_description = f.read()

setup(
    name="flowforge",
    version="1.0.0",
    author="FlowForge Contributors",
    author_email="team@flowforge.dev",
    description="Python SDK for FlowForge — distributed workflow orchestration engine",
    long_description=long_description,
    long_description_content_type="text/markdown",
    url="https://github.com/kasidit-wansudon/flowforge",
    project_urls={
        "Bug Tracker": "https://github.com/kasidit-wansudon/flowforge/issues",
        "Documentation": "https://github.com/kasidit-wansudon/flowforge/tree/main/sdk/python",
        "Source Code": "https://github.com/kasidit-wansudon/flowforge",
    },
    packages=find_packages(),
    python_requires=">=3.9",
    install_requires=[
        "requests>=2.28.0,<3.0.0",
        "urllib3>=1.26.0,<3.0.0",
    ],
    extras_require={
        "dev": [
            "pytest>=7.0",
            "pytest-cov>=4.0",
            "responses>=0.23.0",
            "mypy>=1.0",
            "ruff>=0.1.0",
        ],
    },
    classifiers=[
        "Development Status :: 4 - Beta",
        "Intended Audience :: Developers",
        "License :: OSI Approved :: MIT License",
        "Operating System :: OS Independent",
        "Programming Language :: Python :: 3",
        "Programming Language :: Python :: 3.9",
        "Programming Language :: Python :: 3.10",
        "Programming Language :: Python :: 3.11",
        "Programming Language :: Python :: 3.12",
        "Topic :: Software Development :: Libraries :: Python Modules",
        "Topic :: System :: Distributed Computing",
    ],
    keywords="workflow orchestration automation dag pipeline",
)

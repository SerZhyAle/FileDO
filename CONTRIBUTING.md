# Contributing to FileDO

We love your input! We want to make contributing to FileDO as easy and transparent as possible, whether it's:

- Reporting a bug
- Discussing the current state of the code
- Submitting a fix
- Proposing new features
- Becoming a maintainer

## Development Process

We use GitHub to host code, to track issues and feature requests, as well as accept pull requests.

## Pull Requests

Pull requests are the best way to propose changes to the codebase. We actively welcome your pull requests:

1. Fork the repo and create your branch from `main`.
2. If you've added code that should be tested, add tests.
3. If you've changed APIs, update the documentation.
4. Ensure the test suite passes.
5. Make sure your code lints.
6. Issue that pull request!

## Any Contributions You Make Will Be Under the MIT Software License

In short, when you submit code changes, your submissions are understood to be under the same [MIT License](http://choosealicense.com/licenses/mit/) that covers the project. Feel free to contact the maintainers if that's a concern.

## Report Bugs Using GitHub's [Issues](https://github.com/yourusername/FileDO/issues)

We use GitHub issues to track public bugs. Report a bug by [opening a new issue](https://github.com/yourusername/FileDO/issues/new); it's that easy!

## Write Bug Reports With Detail, Background, and Sample Code

**Great Bug Reports** tend to have:

- A quick summary and/or background
- Steps to reproduce
  - Be specific!
  - Give sample code if you can
- What you expected would happen
- What actually happens
- Notes (possibly including why you think this might be happening, or stuff you tried that didn't work)

People *love* thorough bug reports. I'm not even kidding.

## Development Setup

1. Install Go 1.19 or later
2. Clone the repository:
   ```bash
   git clone https://github.com/yourusername/FileDO.git
   cd FileDO
   ```
3. Install dependencies:
   ```bash
   go mod download
   ```
4. Build the project:
   ```bash
   go build -o filedo.exe
   ```

## Code Style

- Use `gofmt` to format your code
- Follow Go conventions and best practices
- Use meaningful variable and function names
- Add comments for complex logic
- Keep functions focused and modular

## Testing

- Add unit tests for new functionality
- Ensure all existing tests pass
- Test on different Windows versions if possible
- Test with various storage devices and network configurations

## Feature Requests

We're always looking for suggestions to make FileDO better. If you have an idea:

1. Check if the feature already exists
2. Check if someone has already requested it in issues
3. If not, open a new issue with:
   - Clear description of the feature
   - Why you think it would be useful
   - Example usage scenarios

## Code Review Process

The core team looks at Pull Requests on a regular basis. After feedback has been given we expect responses within two weeks. After two weeks we may close the pull request if it isn't showing any activity.

## Community

- Be respectful and inclusive
- Help others learn and grow
- Share your knowledge and experience
- Be patient with newcomers

## Security Issues

If you discover a security issue, please email sza@ukr.net instead of using the issue tracker.

## License

By contributing, you agree that your contributions will be licensed under its MIT License.

## Recognition

Contributors who make significant contributions will be recognized in the project's documentation and release notes.

Thank you for contributing to FileDO! 🚀
